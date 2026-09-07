# gotto-hando — 컨셉 문서

> AI 에이전트가 shell에서 한 줄 문법으로 데스크톱(GUI)을 제어하게 하는 순수 CLI 도구.
> `gotto-hando <pc-profile> <line>...` 한 번의 호출로 키 입력·마우스·윈도우 포커스·화면 캡처를 순차 실행한다.

이 문서는 `gunpowder-odyssey/tools/cua-batch`(Python, JSON 입력, SSH+Cua 기반 Windows 원격 제어)를 참조 구현으로 분석한 뒤,
그 기능 집합을 계승하면서 **JSON 없는 커스텀 문법**, **단일 정적 바이너리**, **IR + backend + transport 계층 분리**, **`--help` = 풀 매뉴얼**(플랫폼·원격 설정은 `--help-macos`/`--help-windows`/`--help-remote`로 분리)
원칙으로 재설계한 결과다.

---

## 목차

1. [참조 구현(cua-batch) 분석 요약](#1-참조-구현cua-batch-분석-요약)
2. [제품 개요](#2-제품-개요)
3. [`--help` = 풀 매뉴얼 원칙](#3---help--풀-매뉴얼-원칙)
4. [시퀀스 문법 스펙](#4-시퀀스-문법-스펙)
5. [커맨드 레퍼런스](#5-커맨드-레퍼런스)
6. [실행 모델](#6-실행-모델)
7. [CLI 인터페이스와 출력 형식](#7-cli-인터페이스와-출력-형식)
8. [아키텍처: IR + backend + transport](#8-아키텍처-ir--backend--transport)
9. [플랫폼별 구현 노트](#9-플랫폼별-구현-노트)
10. [원격 실행과 보안](#10-원격-실행과-보안)
11. [cua-batch ↔ gotto-hando 매핑](#11-cua-batch--gotto-hando-매핑)
12. [리포 구조 초안](#12-리포-구조-초안)
13. [로드맵](#13-로드맵)
14. [Open questions](#14-open-questions)
- [부록 A. `--help` 계열 본문 초안](#부록-a---help-계열-본문-초안)
  - [A.1 `--help`](#a1---help)
  - [A.2 `--help-macos`](#a2---help-macos)
  - [A.3 `--help-windows`](#a3---help-windows)
  - [A.4 `--help-remote`](#a4---help-remote)
- [부록 B. IR JSON 예시](#부록-b-ir-json-예시)

---

## 1. 참조 구현(cua-batch) 분석 요약

### 1.1 파일별 역할

| 파일 | 역할 | gotto-hando에서의 대응 |
|---|---|---|
| `cua_batch.py` | argparse CLI. `batch`/`capture` 서브커맨드, `--json`/`--file`/`--validate`/`--summary`. `MANUAL` 문자열을 `--help` epilog로 출력 | `cmd/gotto-hando` + `internal/help` |
| `schema.py` | 액션 union의 엄격한 스키마 검증(필수/선택 필드, 범위, 라벨 문자 집합, held 버튼 소유권 검사, 총 시간·캡처 수 예산) | `internal/ir` + `internal/syntax` 검증 단계 |
| `prepare.py` | `paste_file`을 클라이언트에서 읽어 `paste{text}`로 정규화 (UTF-8, 16384자/65536바이트 제한) | 파서의 `[f]` 플래그 처리 |
| `engine.py` | 순차 실행기. held 키/버튼 추적, 실패 시 역순 릴리즈, 최종 캡처, 타이밍 기록 | `internal/engine` |
| `backend.py` | Cua 드라이버(`cua-driver.exe call <tool>`)로 캡처·스크롤·클립보드, `Native`로 입력. preflight(물리 키 눌림, 좌표 범위, 키 이름, Cua 스키마) | `internal/backend` 인터페이스 |
| `native.py` | Windows `SendInput` 어댑터. 세션/데스크톱 잠금 확인, 확장키·키패드 스캔코드, 유니코드 텍스트, 폴리라인 드래그 보간 | `internal/backend/windows` |
| `transport.py` | SSH 1회로 부트스트랩(PowerShell EncodedCommand + stdin JSON) → 원격 임시 폴더 → `scp -r`로 캡처 회수 | `internal/transport` (HTTP over SSH 터널로 대체) |
| `worker.py` | 원격 인터랙티브 세션 워커. `started`/`cancel`/`result.json` 파일 프로토콜, 360s 데드라인, `FreeConsole` | `--serve` 상주 서버로 대체 |
| `test_*.py` | 오프라인 계약 테스트 17+개 | `internal/engine` 테이블 테스트로 이식 |

### 1.2 액션 union 전체 목록 (schema.py `FIELDS` 기준)

| type | 필수 | 선택(기본값) | 비고 |
|---|---|---|---|
| `click` | `x,y` | `button`(left) | move → down → up |
| `double_click` | `x,y` | `button`(left) | down/up 2회 |
| `mouse_move` | `x,y` | | 절대 좌표, 가상 데스크톱 음수 원점 허용 |
| `mouse_down` | `button` | | 이미 held면 거부 |
| `mouse_up` | `button` | | 선행 `mouse_down` 없으면 거부 |
| `drag` | `path`(2..200점) | `button`(left), `duration`(0.5s, ≤10), `steps`(20, 1..200) | 폴리라인 균등 보간, 실패해도 버튼 릴리즈 |
| `scroll` | `direction` | `amount`(3, 1..50), `by`(line/page), `x,y` | Cua 경유, foreground |
| `key` | `keys[]`(1..8, 유일) | | 순서대로 press, 역순 release. `enter`→`return` 정규화 |
| `text` | `text`(≤16384) | | `KEYEVENTF_UNICODE` 단위별 down/up |
| `paste` | `text` | | 타깃 클립보드 교체 후 Ctrl+V. Return은 보내지 않음 |
| `paste_file` | `path` | | 클라이언트 로컬 파일 → `paste` |
| `wait` | `seconds`(≤60) | | |
| `screenshot` | | `label`(screenshot) | 네이티브 PNG |
| `capture_frames` | `count`(1..120), `interval_ms`(0..10000) | `label`(frame) | 절대 데드라인 기준, slippage 보고 |

공통: 모든 액션에 `delay`(0..10s, 액션 완료 후 대기) 허용, 배치 전역 `action_delay`(기본 0.33s). 배치 제한: 100 액션, 캡처 120+1, 요청 시간 합 300s. 라벨은 `[A-Za-z0-9][A-Za-z0-9_-]{0,47}` (경로 탈출 방지).

### 1.3 실행 모델에서 계승할 것

- **전체 검증 후 실행**: 스키마/파일 오류는 원격 접촉 전에 거부(`--validate`는 완전 오프라인).
- **held 상태 추적**: 키/버튼 down은 `held` 리스트에 기록. `key` 액션은 press 순서대로 누르고 역순 release. 코드(chord)는 이전 액션이 held 중인 마우스 버튼을 보존한다.
- **fail-fast + 정리**: 액션 실패 시 이후 액션 중단 → held 전부 역순 release → 최종 캡처 시도. release 실패도 최종 캡처를 막지 않는다. (gotto-hando는 암묵 최종 캡처를 없애고 `--cap-on-error`로 옵트인한다 — 6.3, 11장 참조.)
- **클립보드 원자성**: 클립보드 쓰기 실패 시 Ctrl+V를 보내지 않는다(옛 내용이 붙여넣어지는 사고 방지). 키 down 실패 시 이미 누른 키는 반드시 release.
- **타이밍 기록**: 액션별 `started_s/completed_s/delay_completed_s`, 캡처별 `scheduled/started/completed/slippage`.
- **입력 수락 ≠ 애플리케이션 반영**: 결과에 `effect: unverified`를 명시. 확인은 캡처로.
- **최종 캡처 파일 경로만 출력, base64/자동 열기 없음**.

### 1.4 계층 구조에서 얻는 힌트

cua-batch는 이미 `schema(IR) → engine(시퀀서) → backend(OS 어댑터) → transport(원격)` 4층이 분리되어 있다. JSON 액션 union이 사실상 IR이며, `engine.execute(batch, backend, clock)`가 backend·clock을 주입받아 오프라인 테스트가 가능한 구조다. gotto-hando는 이 분리를 그대로 가져가되, (1) IR 앞에 **텍스트 파서** 층을 추가하고, (2) transport를 "SSH 1회 부트스트랩 + scp"에서 "상주 서버 + HTTP over SSH 터널"로 바꾼다.

### 1.5 테스트/README에서 드러난 함정 (모두 `--help` 계열 caveat에 반영)

| 함정 | 출처 | 반영 |
|---|---|---|
| Windows 유니코드 `SendInput`은 Blender 등 스캔코드 기반 앱에서 무시됨. 숫자 변환은 개별 키 이벤트로 | README, MANUAL | `txt` caveat, `k`로 대체 안내 |
| 키패드는 `KEYEVENTF_SCANCODE`(flag 8)로 NumLock과 무관하게 물리 위치 전송, 화살표/Home/End 등은 `EXTENDEDKEY` 필요 | `native.key`, `test_native_key_preserves_keypad_scan_and_release` | Windows backend 요구사항 |
| 마우스 이동은 `MOUSEEVENTF_ABSOLUTE\|VIRTUALDESK`로 정규화(0..65535), 가상 데스크톱 원점이 음수일 수 있음 | `native.move`, `bounds` | 좌표 공간 정의 |
| 워커가 활성 콘솔 세션(`WTSGetActiveConsoleSessionId`)이 아니거나 데스크톱이 `Default`가 아니면(잠김/UAC) 입력 불가 | `check_session` | RDP/잠금 caveat, `qinfo`의 `session` 필드 |
| SSH로 실행된 프로세스는 인터랙티브 세션이 아니므로 Cua `launch_app`으로 워커를 다시 띄우고 `FreeConsole`로 콘솔을 뗌 | transport BOOTSTRAP, worker | `--serve`를 GUI 세션에서 상주시키는 설계로 근본 해결 |
| 붙여넣기는 Return을 보내지 않지만 콘솔은 개행을 실행할 수 있음. Blender Console에는 한 줄 `exec(compile(...))`로 | README | `paste` caveat |
| 클립보드 쓰기 실패 시 붙여넣기 금지, 키 실패 시 소유 키 release | `test_paste.py` | engine 규칙 |
| `paste_file` 내용이 셸 커맨드 소스에 섞이면 안 됨(`$HOME`, backtick 등) | `test_transport_sends_file_contents_as_data_only` | IR은 항상 데이터 채널(HTTP body)로만 전달 |
| 라벨에 `../` 금지, 캡처 파일명 검증, 원격 디렉터리 경로 검증 | schema, transport | 캡처 경로는 클라이언트가 결정 |
| 물리적으로 키/버튼이 눌린 상태에서 시작하면 거부 | `Backend.preflight` | preflight 규칙 |
| 프레임 캡처 타이밍은 best-effort, 첫 캡처 1s 지연 사례 | README 타이밍 증거 | `cap[n=]` caveat |
| `start_minimized` 등 드라이버 옵션이 성공 코드로도 실패 가능 | README | 응답 검사 로직(`check_response`)을 backend 결과에 반영 |
| 순간 캡처 66ms, 왕복 1.8s(SSH 부트스트랩 포함) | README | `--serve` 상주로 왕복 비용 제거가 목표 |

---

## 2. 제품 개요

### 2.1 한 줄 정의

`gotto-hando`는 **non-MCP, 순수 CLI** 데스크톱 제어 바이너리다. 에이전트는 툴 프로토콜 없이 shell에서 다음처럼 호출한다.

```sh
gotto-hando local 'win[]Blender' 'k[c]a' 'txt[ms=20]hello' 'cap'
gotto-hando winbox -f steps.txt
printf '%s\n' 'm[]1024,133' 'c' 'cap' | gotto-hando winbox
```

- 첫 번째 인자 `<pc-profile>`: 제어 대상 PC. `local`은 예약(현재 머신, in-process). 그 외는 프로파일 설정에 등록된 원격 엔드포인트.
- 이후 인자 각각이 **한 줄 = 한 명령**인 시퀀스. stdin/파일도 지원.
- 한 번의 실행 안에서 상태 머신으로 동작: 선택된 윈도우, held 키/버튼, 지연 설정 등이 다음 줄로 이어진다.

### 2.2 cua-batch와 다른 점

| 항목 | cua-batch | gotto-hando |
|---|---|---|
| 입력 | JSON (`{"type":"key","keys":["ctrl","v"]}`) | 한 줄 문법 (`k[c]v`) |
| 언어/배포 | Python 3.10+, Cua 드라이버 의존 | Go 단일 정적 바이너리, 외부 의존 없음 |
| 플랫폼 | Windows 타깃(클라이언트는 Mac) | macOS + Windows 양쪽 타깃, 양쪽 클라이언트 |
| 원격 | 배치마다 SSH 부트스트랩 + 워커 launch + scp | `--serve` 상주 서버 + SSH 터널 + HTTP |
| 윈도우 제어 | 없음(항상 foreground) | 윈도우 조회/포커스/윈도우 기준 좌표 |
| 캡처 | 전체 화면 PNG만 | 전체/디스플레이/윈도우/영역, 스케일, 포맷 |
| 임의 실행 | 없음(부트스트랩용 SSH+PowerShell은 내부 구현) | `exec`(명령 실행)·`open`(앱 실행) — 서버 계정의 셸과 동등(5.4, 10.1) |
| 도움말 | `--help` epilog | `--help` = 풀 매뉴얼 (사용법·문법·레퍼런스·caveat·예제). 플랫폼·원격 설정은 `--help-macos`/`--help-windows`/`--help-remote` (3장) |

### 2.3 언어 선택: Go (권장)

| 기준 | Go | Rust | 비고 |
|---|---|---|---|
| 단일 정적 바이너리 | 기본 | 기본 | 동률 |
| cross-compile | `GOOS/GOARCH`만으로 됨. 단 cgo 사용 시 타깃 툴체인 필요 | `cross`/zig 등 필요 | Go 우세. macOS 네이티브 API는 cgo(Objective-C) 필요 → macOS 빌드는 macOS 호스트에서, Windows 빌드는 `syscall`만으로 cgo 없이 가능 |
| 데스크톱 자동화 생태계 | `robotgo`(cgo, 통합 패키지), `golang.org/x/sys/windows`, 직접 cgo | `enigo`, `windows-rs`, `core-graphics` | 둘 다 충분. Go는 `x/sys/windows`로 `SendInput`/`user32` 직접 호출이 쉬움 |
| HTTP 서버/클라이언트 | 표준 라이브러리 | `tokio`+`hyper` 등 의존 | Go 우세 |
| 빌드 속도/개발 속도 | 빠름 | 느림 | Go 우세 |
| 안전성/성능 | 충분(입력 이벤트는 저부하) | 더 강함 | 이 용도엔 무의미 |

결정: **Go**. `robotgo`는 의존 트리가 크고(libpng, X11 헤더 등) 동작을 세밀히 제어하기 어려우므로, **플랫폼 API 직접 호출**(macOS: cgo로 Quartz/AppKit, Windows: `x/sys/windows`로 cgo 없이)을 기본안으로 한다. 후보 API:

| 기능 | macOS | Windows |
|---|---|---|
| 키/마우스 입력 | `CGEventCreateKeyboardEvent`, `CGEventCreateMouseEvent`, `CGEventCreateScrollWheelEvent`, `CGEventPost(kCGHIDEventTap)`, 텍스트는 `CGEventKeyboardSetUnicodeString` | `SendInput` (`KEYEVENTF_UNICODE`, `KEYEVENTF_SCANCODE`, `KEYEVENTF_EXTENDEDKEY`, `MOUSEEVENTF_ABSOLUTE\|VIRTUALDESK`, `MOUSEEVENTF_WHEEL/HWHEEL`) |
| 윈도우 조회 | `CGWindowListCopyWindowInfo`, `AXUIElement`(Accessibility) | `EnumWindows`, `GetWindowTextW`, `IsWindowVisible`, `DwmGetWindowAttribute(DWMWA_CLOAKED)`, `GetWindowThreadProcessId` |
| 윈도우 포커스 | `AXUIElementSetAttributeValue(kAXMainAttribute/kAXFocusedAttribute)`, `NSRunningApplication.activate` | `SetForegroundWindow` + `AttachThreadInput`/Alt 키 트릭, `ShowWindow(SW_RESTORE)` |
| 캡처 | `ScreenCaptureKit`(macOS 12.3+), 폴백 `CGWindowListCreateImage`(14에서 deprecated), 최후 폴백 `/usr/sbin/screencapture` | GDI `BitBlt`(가상 화면 DC), 윈도우는 `PrintWindow(PW_RENDERFULLCONTENT)`, 선택적 DXGI Desktop Duplication |
| 디스플레이/DPI | `CGGetActiveDisplayList`, `CGDisplayBounds`, backing scale | `EnumDisplayMonitors`, `GetDpiForMonitor`, 프로세스는 Per-Monitor-V2 DPI aware |
| 클립보드 | `NSPasteboard` | `OpenClipboard`/`SetClipboardData(CF_UNICODETEXT)` |
| 세션 상태 | GUI 세션 여부(`CGSessionCopyCurrentDictionary`), 권한(`AXIsProcessTrusted`, `CGPreflightScreenCaptureAccess`) | `ProcessIdToSessionId` == `WTSGetActiveConsoleSessionId`, `OpenInputDesktop` 이름 == `Default` |

---

## 3. `--help` = 풀 매뉴얼 원칙

에이전트는 사전 지식 없이 `gotto-hando --help` 한 번으로 **도구를 쓰는 데 필요한 모든 것**을 배워야 한다. 따라서 `--help`는 옵션 요약이 아니라 **사용법(synopsis/옵션/출력/exit code) + 문법 스펙 + 전체 커맨드 레퍼런스 + caveat + 예제**를 포함한 단일 문서다. 이 문서 5장의 레퍼런스와 부록 A.1의 초안이 그 원본이며, 빌드 시 `embed`로 바이너리에 포함된다. `--help` 계열은 모두 **영어**로 작성한다(확정; 에이전트 대상이므로 토큰 효율과 앱 문서 용어와의 일치가 우선).

### 3.1 네 개의 도움말 (확정)

플랫폼 설정과 원격 설정은 `--help`에서 **제외**하고, 필요할 때만 읽는 opt-in 플래그로 분리한다. 각각 독립된 풀 텍스트이며 서로 중복하지 않는다(상호참조만).

| 플래그 | 내용 | 언제 읽는가 |
|---|---|---|
| `--help` | 사용법·문법·커맨드 레퍼런스·키 이름·상태 머신·출력 형식·제한·caveat·셸 인용·프로파일 개요·예제·exit code. 끝에 `== SEE ALSO ==`로 아래 셋을 안내 | 항상. 도구를 처음 쓸 때 |
| `--help-macos` | macOS 로컬 빌드/서명, TCC 권한(접근성·화면 기록)과 서명 identity의 관계, 책임 프로세스 규칙, 원격 Mac에서의 수동 권한 부여, `--request-perms` 절차, 세션/Secure Input/Retina·다중 디스플레이 caveat | 권한 오류(`E_PERMISSION`, exit 4)나 `qinfo`의 `perms=...:missing`이 나올 때, macOS를 재빌드했을 때, macOS에 `--serve`를 띄울 때 |
| `--help-windows` | 대화형 세션 요건(세션 0·RDP·잠금), 작업 스케줄러로 `--serve` 상주, SmartScreen/Defender, UAC/UIPI, DPI와 좌표, `exec[shell]`의 `cmd /C`와 코드페이지 | `E_SESSION`이나 `qinfo`의 `session=`이 `active`가 아닐 때, Windows에 `--serve`를 띄울 때, 관리자 권한 앱에 입력이 안 들어갈 때 |
| `--help-remote` | 서버 측 설치·`--serve`·`--token`, 클라이언트 측 `ssh -L`·프로파일 등록·`qinfo` 확인, localhost-only/`--bind` 없음/SSH 터널 필수, "서버 = 그 계정의 셸" 경고 | `local`이 아닌 다른 PC를 제어하려 할 때, exit 3(`E_CONNECT`)이 날 때 |

- `--help [topic]`(섹션만 출력하는 토픽 인자) 방식은 **채택하지 않는다**. `--help`는 항상 전체를 출력하며, 에이전트는 고정 헤딩(`== SECTION ==`)을 `grep`해서 섹션을 찾는다. 세 플랫폼/원격 플래그가 토픽 분리의 유일한 형태다.
- 네 플래그는 모두 `<profile>` 없이 단독으로 쓰며, 다른 옵션·줄과 함께 주면 무시하고 도움말만 출력한다(exit 0).
- 이 문서의 원본 위치: `--help` → 부록 A.1, `--help-macos` → A.2(9.1의 사용자용 요약), `--help-windows` → A.3(9.2와 10.3의 Windows 행), `--help-remote` → A.4(10.2). 본문(9·10장)은 구현 노트이고, 부록 A가 에이전트에게 보이는 최종 텍스트다.

### 3.2 에이전트 가독성 기준

1. **예제 우선**: 각 섹션 첫 줄이 실행 가능한 예제.
2. **구조화된 섹션 + 고정 헤딩**: 에이전트가 `grep`으로 섹션을 찾을 수 있게 `== SECTION ==` 형태의 헤딩. 네 도움말 모두 같은 형식.
3. **표는 고정폭 텍스트**: 커맨드 / 모디파이어 / 페이로드 / 예제 4열.
4. **함정은 `CAVEAT:` 접두어로 명시**, 권장 대안 동반. 플랫폼 고유 함정은 `--help`에 한 줄 포인터만 두고 본문은 해당 플랫폼 도움말에.
5. **정확한 실패 조건**: 어떤 입력이 exit 2(파싱 오류)인지, 어떤 상태가 exit 4(권한/세션)인지.
6. **절차형(플랫폼/원격 도움말)**: 에이전트가 읽고 스스로 조치할 수 있도록 "확인 명령 → 판정 → 조치 → 안 되면 사용자에게 안내할 문구" 순서로 쓴다. 사람이 클릭해야만 되는 단계(macOS TCC 토글, Windows SmartScreen "실행")는 **자동화 불가**라고 명시한다.
7. **길이 제한 없음**: 수백 줄이어도 좋다. 다만 네 텍스트 사이에 중복 없이.

### 3.3 `--help` 목차 초안

```
gotto-hando — drive a desktop (macOS/Windows) from the shell, one command per line

== SYNOPSIS ==            호출 형태, 프로파일, 입력 소스(argv/stdin/file), 옵션 요약, 도움말 플래그 4종
== QUICK START ==         5줄짜리 첫 예제 + 출력 예시
== SYNTAX ==              한 줄 문법 정식 정의(EBNF), 형태 판별 규칙(bracket-form 필수), 이스케이프, 좌표, 시간 단위
== MODIFIER KEYS ==       c/s/a/m/p 의미, 플랫폼 매핑 표
== COMMANDS ==            전체 커맨드 레퍼런스 표 (keyboard / mouse / clipboard / window·app·process / capture / flow / query)
== KEY NAMES ==           `k`/`kd`/`ku`에서 쓰는 키 이름 전체 목록
== STATE MACHINE ==       한 실행 안에서 이어지는 상태, held 자동 해제, 기본 지연
== OUTPUT ==              stdout 라인 형식, --jsonl, exit code
== LIMITS ==              길이/시간/개수 제한
== CAVEATS ==             앱/문법 수준 함정과 대안. 플랫폼 함정은 한 줄 포인터
== SHELL QUOTING ==       [ ] ! 공백 처리, single quote / heredoc / -f 권장
== PROFILES ==            <profile> 인자의 의미, profiles.toml 위치, local 예약, --profiles (형식 상세는 --help-remote)
== EXAMPLES ==            시나리오별 시퀀스 10여 개
== EXIT CODES ==
== SEE ALSO ==            --help-macos / --help-windows / --help-remote 와 각각이 필요한 상황 한 줄씩
```

`--help-macos`, `--help-windows`, `--help-remote`의 목차는 부록 A.2~A.4에 있다.

---

## 4. 시퀀스 문법 스펙

### 4.1 개요

- 입력은 UTF-8 텍스트. **개행(`\n`, `\r\n` 허용)으로 구분된 각 줄이 하나의 명령**이다.
- 빈 줄(공백만)은 무시. `#`으로 시작하는 줄은 주석(첫 비공백 문자가 `#`).
- 한 줄의 형태는 **두 가지뿐**: `<command>[<modifiers>]<payload>`(bracket-form) 또는 `<command>`(EOL 직후, payload 없음). `<command> <payload>`처럼 공백으로 구분하는 형태(space-form)는 **문법 오류**다. payload가 있으면 대괄호는 비어 있더라도 필수(`m[]1024,133`, `k[]enter`).
  - 결정 근거: 파싱 규칙이 하나로 줄고, "공백 정확히 1개" 규칙과 텍스트 계열의 선행 공백 보존 caveat이 사라진다. payload는 항상 첫 `]` 직후부터 EOL까지이므로 위치가 언제나 명확하다. 비용은 줄당 2문자.
- 시퀀스 전체가 파싱·검증된 뒤에야 실행이 시작된다. 한 줄이라도 문법 오류면 아무것도 실행하지 않는다(exit 2).

### 4.2 정식 정의 (EBNF)

```ebnf
sequence     = { line , EOL } , [ line ] ;
line         = ws , ( empty | comment | statement ) ;
empty        = "" ;
comment      = "#" , { any } ;
statement    = command , [ bracket-form ] ;             (* payload가 있으면 bracket-form 필수. space-form 없음 *)

command      = lower , { lower | digit } ;              (* e.g. k kd txt m c cap qwin exec *)
bracket-form = "[" , [ modifiers ] , "]" , payload ;    (* 첫 "]" 뒤 EOL까지 전부 payload. "[]"(모디파이어 없음)도 유효 *)

modifiers    = modifier , { "," , modifier } ;
modifier     = kv | flags ;
kv           = key , "=" , value ;
key          = lower , { lower | digit } ;
value        = item , { ":" , item } ;                  (* 리스트 값은 ":"로 구분(rect=0:0:600:400). 인용/이스케이프 없음 *)
item         = { any - ( "," | ":" | "]" | EOL ) } ;
flags        = flag , { flag } ;                        (* "cs" == "c,s" *)
flag         = lower ;                                  (* 커맨드별로 허용 집합이 다름 *)

payload      = { any - EOL } ;                          (* 커맨드별 하위 문법 적용 *)

ws           = { " " | TAB } ;
EOL          = "\n" | "\r\n" ;
lower        = "a" … "z" ;  digit = "0" … "9" ;
```

**형태 판별 규칙**: `command` 토큰 직후의 문자 하나로 결정한다.

| 직후 문자 | 형태 | payload 시작 |
|---|---|---|
| `[` | bracket-form | 첫 번째 `]` 바로 다음 문자부터 EOL까지 (raw) |
| EOL(후행 공백만 있는 경우 포함) | payload 없음 | — |
| 그 외 (`m1024,133`, `m 1024,133`, `k enter`) | **문법 오류** (`E_SYNTAX`, exit 2) | — |

- 빈 `[]`는 "모디파이어 없음"을 뜻한다: `m[]1024,133`, `k[]enter enter`, `sleep[]500`. space-form(`m 1024,133`)은 존재하지 않는다(4.1).
- modifiers 안의 `value`에는 `,`와 `]`가 올 수 없다. **value 내부의 리스트는 `:`로 구분**한다(`cap[rect=0:0:600:400]`; 현재 리스트 값을 받는 key는 `rect=`뿐). 경로처럼 긴 값은 payload로 받는다(예: `cap` 출력 경로, `paste[f]` 파일 경로).
- 모디파이어는 `,`로 나눈 토큰 단위로 해석한다. 토큰에 `=`가 있으면 kv, 없으면 flag 묶음이다(`[ms]` = flag `m`+`s`, `[ms=20]` = kv `ms`). 같은 flag/`key`가 두 번 나오면 문법 오류. 알 수 없는 flag/key도 문법 오류(엄격 모드; 오타를 조용히 무시하지 않는다).
- **payload는 EOL까지 raw**다. 텍스트 계열 커맨드(`txt`, `paste`, `clip`)에서는 앞뒤 공백이 그대로(verbatim) 유효하며, 그 외 커맨드에서는 앞뒤 공백을 trim한 뒤 하위 문법을 적용한다. 단 **`[f]`가 붙으면 payload는 파일 경로이므로 텍스트 계열이라도 trim**한다.
  - `txt[]  two spaces` → `"  two spaces"` 타이핑
  - `paste[f] ./a.py ` → 파일 경로 `./a.py`
- 텍스트 계열 커맨드의 payload에 `[`, `]`가 있어도 무방하다: `txt[]a[b]c` → `a[b]c`. 파서는 첫 `]`에서만 멈춘다.

### 4.3 공통 모디파이어

| 종류 | 표기 | 의미 | 허용 커맨드 |
|---|---|---|---|
| flag | `c` | Control 키를 명령 동안 hold | 입력 커맨드(`k c md mu drag scroll m`) |
| flag | `s` | Shift | 〃 |
| flag | `a` | Alt (macOS: Option) | 〃 |
| flag | `m` | Meta (macOS: Command ⌘, Windows: Win ⊞) | 〃 |
| flag | `p` | **Primary** — macOS: Command, Windows: Control | 〃 |
| kv | `d=<dur>` | 이 명령 완료 후 대기 시간(전역 `delay` 덮어씀) | 모든 커맨드 |
| kv | `ms=<dur>` | 커맨드 고유 타이밍(의미는 커맨드별: 간격/지속/정착 시간) | 커맨드별 |
| flag | `f` | payload가 **클라이언트 로컬 파일 경로**임(내용을 읽어 사용/저장) | `txt paste clip qclip` |
| flag | `r` | 좌표: 현재 포인터 기준 상대 좌표 / `win`·`qwin`: 정규식 매칭 | 좌표 커맨드 / 윈도우 커맨드 |
| flag | `w` | 좌표·캡처의 기준 프레임 = 현재 선택 윈도우(`win` 미실행이면 OS 포커스 윈도우, 6.2) | 좌표 커맨드, `cap` |

모디파이어 키 flag(`c s a m p`)는 **payload 전체가 실행되는 동안** 눌린 상태로 유지되며 명령이 끝나면 역순으로 해제된다. 순서: `c → s → a → m/p` 순으로 누르고 역순으로 뗀다.

### 4.4 모디파이어 키 추상화 결정 (macOS `cmd` vs Windows `ctrl`)

**결정: `c`는 항상 문자 그대로 Control, `m`은 문자 그대로 Meta(Cmd/Win). 플랫폼 기본 modifier가 필요하면 `p`(primary)를 쓴다.**

근거:

1. **예측 가능성**: 에이전트가 앱 문서의 "Ctrl+V"/"⌘V"를 그대로 옮기면 OS가 그 키를 받는다. 숨은 변환이 없어 디버깅이 단순하다.
2. **macOS의 진짜 Control**: 터미널 `^C`, `^A`/`^E`, emacs 바인딩, `ctrl+space`(입력 소스 전환) 등은 Control이어야 한다. `c`를 primary로 매핑하면 macOS 터미널에서 `k[c]c`가 Cmd+C(복사)로 둔갑해 프로세스 중단이 조용히 실패하는 최악의 사고가 난다.
3. **이식성은 `p`로**: 플랫폼을 모르고 쓰는 시퀀스는 `k[p]v`, `k[p]s`처럼 `p`를 쓰면 된다. `paste` 커맨드는 내부적으로 `p+v`를 쓴다.
4. **OS 확인 수단 제공**: `gotto-hando <profile> qinfo`가 `os=darwin primary=cmd`를 출력하므로 에이전트는 언제든 확인할 수 있다.

기각한 대안: "`c` = primary, `ctrl`은 별도 문자(`x`)" — 사용자 예시 `k[c]v = ctrl+v`와 Windows 관습에는 맞지만 위 2번 사고를 유발하고, 문자 `x`의 발견 가능성이 낮다.

Win 키만을 뜻하는 별도 flag는 제공하지 않는다(확정): Meta는 `m`뿐이고, `w`는 window 프레임 flag다(4.3). 키 이름 `win`(=`meta`=`cmd`)은 payload에서 그대로 쓸 수 있다(`k[]win`).

### 4.5 좌표 문법

```ebnf
point   = coord , "," , coord ;
coord   = [ "-" ] , number , [ "%" ] ;
points  = point , { ws , point } ;        (* drag *)
rect    = coord , ":" , coord , ":" , number , ":" , number ;   (* x:y:w:h — 모디파이어 value이므로 ":" 구분(4.2) *)
```

- 단위: **논리 좌표**. macOS는 points(Retina에서 픽셀의 1/2), Windows는 물리 픽셀(프로세스가 Per-Monitor-V2 DPI aware). 캡처 이미지는 기본적으로 **이 좌표계와 1:1**이 되도록 저장된다(5.5 참조). 즉 캡처 이미지에서 찾은 픽셀 좌표를 그대로 입력에 쓰면 된다.
- 기준 프레임은 모디파이어로 선택하며 좌표 문자열 자체는 항상 `x,y` 숫자다.

| 모디파이어 | 기준 | 예 |
|---|---|---|
| (없음) | 가상 데스크톱 절대 좌표. 원점 = 주 디스플레이 좌상단, y는 아래로 증가. 다중 모니터에서 음수 가능 | `m[]1024,133` |
| `r` | 현재 포인터 위치 기준 상대 이동 | `m[r]10,-5` |
| `w` | 현재 선택 윈도우 프레임(타이틀바 포함)의 좌상단 기준 | `c[w]200,80` |
| `disp=N` | N번째 디스플레이(0부터, `qdisp` 순서)의 좌상단 기준 | `m[disp=1]0,0` |
| `%` 접미 | 기준 프레임 크기 대비 백분율(소수 허용) | `c[w]50%,50%` (윈도우 중앙) |

- 좌표가 기준 프레임 밖이면 `E_BOUNDS`. 절대·`disp=` 좌표는 backend preflight에서 검사해 아무것도 실행하지 않고 exit 4(6.1 4단계). `r`/`w`/`%`처럼 실행 시점 상태(포인터·윈도우)에 의존하는 좌표는 해당 줄 실행 직전에 검사하며 실패하면 그 줄이 `err`(exit 1). `r`는 결과 위치 기준으로 검사. 오프라인 정적 검증(exit 2)은 숫자 형식만 본다(데스크톱 크기를 모르므로).
- 정수/소수 모두 허용, 실행 시 반올림.
- **프레임 모디파이어(`r`, `w`, `disp=`)는 한 명령에 하나만** 허용한다. 둘 이상이면 문법 오류(exit 2). (확정)
- **`%` 좌표는 `r` 프레임과 조합할 수 없다**(상대 이동에는 백분율의 기준 크기가 없음) → 문법 오류(exit 2). (확정)

### 4.6 시간(duration) 문법

`dur = number , [ "ms" | "s" ]`. 접미 없으면 **ms**. 예: `sleep[]500`, `sleep[]1.5s`, `txt[ms=66]`, `k[d=1s]enter`. 음수/NaN/Inf 거부.

### 4.7 텍스트 payload 이스케이프

텍스트 계열(`txt`, `paste`, `clip`)의 payload에만 적용. 그 외 커맨드에서는 `\`가 일반 문자다.

| 시퀀스 | `txt` | `paste`/`clip` |
|---|---|---|
| `\n` | Return 키 입력 | 개행 문자 `\n` |
| `\t` | Tab 키 입력 | 탭 문자 |
| `\\` | `\` 문자 | `\` |
| `\uXXXX` | 해당 유니코드 문자 | 〃 |
| 기타 `\?` | **문법 오류** (예: `txt[]C:\Users` → `\U` 오류. `C:\\Users`로 쓸 것) | 〃 |

`[f]` 플래그가 있으면 payload는 파일 경로(앞뒤 공백 trim)이고 이스케이프를 적용하지 않으며 파일 내용을 그대로 쓴다(UTF-8, 64 KiB 이하). 여러 줄 텍스트는 `[f]`를 쓰는 것이 권장 경로다.

### 4.8 윈도우 선택자(selector)

`win`과 `qwin`의 payload.

| 형태 | 의미 |
|---|---|
| `id:<n>` | `qwin`이 출력한 윈도우 id (가장 확정적) |
| `pid:<n>` | 프로세스 id의 최전면 윈도우 |
| `app:<name>` | 앱/프로세스 이름(대소문자 무시 부분 일치, macOS는 번들 이름, Windows는 exe 이름) |
| 그 외 문자열 | 윈도우 타이틀 부분 일치(대소문자 무시). `r` flag면 정규식(RE2) |

여러 개가 일치하면 z-order 최전면을 선택하고 결과 라인에 `matched=N`을 표기한다. `win[wait=5s]`는 일치 윈도우가 나타날 때까지 폴링(100ms 간격).

### 4.9 셸 인용(quoting) caveat

`[`, `]`, `!`, `*`, `?`, `$`, backtick, 공백, `#`은 셸에서 해석된다. bracket-form이 필수이므로 **payload가 있는 모든 줄에 `[`·`]`가 들어간다** — 인용하지 않으면 glob에 걸린다(zsh는 `no matches found`로 중단, bash는 일치 파일이 있으면 치환). 따라서 항상 인용한다. 권장:

```sh
# 1) 각 줄을 single quote로 — 가장 단순. payload에 single quote가 필요하면 '\''
gotto-hando local 'k[c]a' 'txt[]hello, world!' 'cap'

# 2) 여러 줄은 heredoc + stdin (quote 없는 'EOF'로 셸 확장 차단)
gotto-hando winbox <<'EOF'
win[]Blender
k[c]a
txt[ms=20]hello
cap
EOF

# 3) 파일
gotto-hando winbox -f steps.gh
```

- zsh에서 `!`는 single quote 안에서 안전하다. double quote 안의 `!`는 history expansion되므로 쓰지 말 것.
- 한 argv 안에 개행이 들어 있으면 그 자리에서 줄이 나뉜다(`$'a\nb'` 형태로 두 줄 전달 가능).
- Windows cmd.exe는 single quote를 모른다. PowerShell은 single quote를 지원하지만 argv 재인용 문제가 있어 `-f`/stdin을 권장.
- argv 총 길이 제한(Windows 32K자, macOS 1MiB) 때문에 큰 `paste`는 반드시 `[f]`.

---

## 5. 커맨드 레퍼런스

표기: `flags`는 단문자 플래그, `kv`는 `key=value`. 공통 모디파이어(4.3: `c s a m p`는 입력 커맨드에서, `d=`는 모든 커맨드에서 허용)는 생략. 기본값은 괄호. `(M1)`/`(M3)`/`(M4)`는 13장 로드맵의 해당 마일스톤에서 구현되는 항목. 모든 예제는 bracket-form(4.1)이다.

### 5.1 입력 — 키보드

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `k` | `n=<int>`(1) 반복 횟수, `ms=<dur>`(30) 키 사이 간격 | 키 이름 목록. 공백 구분 = 순차 입력, `+` 결합 = 동시 코드(순서대로 누르고 역순 해제). 코드는 **모디파이어 flag를 포함해** 최대 8키(`k[csam]a` = 5키; OS 동시 hold 한계 기준) | `k[c]v` Ctrl+V. `k[]enter enter` Enter 2회. `k[n=3]tab` Tab 3회. `k[]ctrl+shift+a` (flag 대신 payload 코드) |
| `kd` | — | 키 이름 목록 | 누른 채 유지. 실행 종료/실패 시 자동 해제(경고 출력) |
| `ku` | — | 키 이름 목록 | `kd`로 누른 키만 해제 가능. 아니면 검증 오류 |
| `txt` | `ms=<dur>`(0) 문자 간 간격, `f` | 텍스트(이스케이프 적용) 또는 파일 경로 | 유니코드 문자 단위 입력. `\n`은 Return 키. 모디파이어 키 flag 불가 |

키 이름(대소문자 무시): `a-z`, `0-9`, `f1-f24`, `ctrl` `shift` `alt`(=`option`) `meta`(=`cmd`=`win`) `primary`, `enter`(=`return`), `tab`, `esc`(=`escape`), `space`, `backspace`, `delete`, `insert`, `home`, `end`, `pageup`, `pagedown`, `up` `down` `left` `right`, `period`, `comma`, `minus`, `equal`, `slash`, `backslash`, `semicolon`, `quote`, `grave`, `lbracket`, `rbracket`, `numpad0-9`, `decimal`, `numadd` `numsub` `nummul` `numdiv` `numenter`, `capslock`, `printscreen`, `scrolllock`, `pause`, `volup` `voldown` `mute`. 키 이름은 **물리 키(US 레이아웃 기준 위치)** 를 뜻하며 문자를 뜻하지 않는다. 문자를 넣으려면 `txt`.

### 5.2 입력 — 마우스

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `m` | `ms=<dur>`(0) 이동 지속(0=즉시, >0이면 보간 이동), `r`, `w`, `disp=` | `x,y` | 포인터 이동 |
| `c` | `b=left\|right\|middle`(left), `n=<int>`(1) 클릭 수, `ms=<dur>`(60) 클릭 간격, `r` `w` `disp=` | `x,y` (생략 시 현재 위치) | (이동 후) 클릭. `c[n=2]` 더블클릭, `c[b=right]` 우클릭, `c[s]` Shift+클릭 |
| `md` | `b=`, 좌표 flag | `x,y` (생략 가능) | 버튼 누른 채 유지. 이미 held면 검증 오류 |
| `mu` | `b=` | (없음) | `md`로 누른 버튼 해제 |
| `drag` | `b=`(left), `ms=<dur>`(500) 총 지속, `steps=<int>`(20), 좌표 flag | `x1,y1 x2,y2 [x3,y3 ...]` (1점만 주면 현재 위치에서 시작) | 첫 점 이동 → down → 폴리라인 균등 보간 이동 → up. 도중 실패해도 up 시도 |
| `scroll` | `by=line\|page`(line) | `up\|down\|left\|right [ticks]` (ticks 생략 시 3, 1..50) | 현재 포인터 위치에서 스크롤. 위치를 정하려면 먼저 `m`. 틱 수는 payload로만 지정한다 — `n=` 모디파이어는 없음(`n=`은 다른 커맨드에서 "반복 횟수" 의미로만 유지; 확정). `scroll[]down 2`, `scroll[by=page]down` |

### 5.3 클립보드

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `clip` | `f` | 텍스트/파일 경로 | 타깃 클립보드에 텍스트 설정(붙여넣기 없음) |
| `paste` | `f`, `ms=<dur>`(50) 클립보드 설정 후 키 입력까지 정착 시간 | 텍스트/파일 경로 | `clip` 후 `primary+v`. 클립보드 설정 실패 시 키를 보내지 않음. Return은 보내지 않음. 클립보드는 복원하지 않음 |
| `qclip` | `f` | (없음) 또는 저장할 로컬 파일 경로 | 타깃 클립보드 텍스트 읽기. stdout에 `\n` 이스케이프로 한 줄 출력, `[f]`면 파일 저장 |

터미널 등 Ctrl+V가 붙여넣기가 아닌 앱: `clip[]...` + `k[s]insert` 또는 `k[cs]v` 조합으로 대체.

### 5.4 윈도우·앱·프로세스

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `win` | `r` 정규식, `wait=<dur>`(0) 등장 대기 (M3) | selector | 윈도우를 찾아 **활성화·전면화·최소화 해제** 후 "현재 윈도우"로 선택. 이후 `[w]` 프레임과 `cap[w]`의 기준 |
| `qwin` | `r` | selector (생략 시 전체; 필터는 M3) | 보이는 윈도우 목록. 포커스 윈도우는 `*` 표시 |
| `open` (M1) | `wait=<dur>`(0) 윈도우 등장 대기 | 앱 이름 또는 실행 경로(앞뒤 공백 trim) | 앱 실행/활성화(macOS `open -a <name>` 또는 `open <path>`, Windows `ShellExecute`). 실행 실패는 `err`(E_EXEC). 임의 경로 허용(allowlist 없음; 근거는 아래). 비입력 커맨드이므로 전역 기본 `delay` 제외(명시 `d=`는 적용; 확정) |
| `exec` (M1) | `timeout=<dur>`(10s) 실행 시간 제한(≤ 60s), `shell` 플랫폼 로그인 셸로 실행, `noerr` exit code ≠ 0을 `err`로 취급하지 않음 | 명령줄(앞뒤 공백 trim) | **타깃 머신에서 명령을 실행**하고 종료까지 대기. `shell` 없음: payload를 공백으로 argv 분할(인용은 단순 `"..."`만, 이스케이프·변수 확장 없음) 후 직접 실행. `shell` 있음: macOS는 **로그인 셸** `$SHELL -lc <payload>`(`$SHELL`이 비어 있으면 `/bin/zsh -lc`), Windows는 `cmd /C <payload>`에 통째로 전달(확정; launchd/작업 스케줄러가 띄운 서버의 축소된 PATH를 로그인 셸의 rc가 복구한다). cwd·환경 변수 = 서버 프로세스(`local`이면 클라이언트 프로세스)의 것. stdin은 닫힘. stdout/stderr(각 ≤ 64 KiB; 초과분은 잘리고 `truncated=1`)와 exit code를 결과 줄/JSONL에 포함. Windows에서는 출력 바이트를 콘솔 출력 코드페이지(`GetConsoleOutputCP`, 콘솔이 없으면 ACP)로 디코드해 UTF-8로 변환하고, 디코드 불가 바이트는 U+FFFD로 치환(확정). exit code ≠ 0 → `err`(E_EXEC)로 fail-fast; **`noerr`** flag가 있으면 exit code를 보고만 하고 `ok`로 계속(`grep`/`diff`처럼 1이 정상인 명령용; 확정). `timeout` 초과 → 프로세스 kill 후 `err`(E_TIMEOUT), 이것은 `noerr`와 무관하게 fail-fast(`-k`로 계속) |

- **임의 실행 허용 근거(확정)**: 원격 사용은 SSH 터널이 필수 조건이므로 사용자는 이미 그 계정의 원격 셸을 가지고 있다. `exec`/`open`은 새로운 권한을 추가하지 않고 왕복(별도 `ssh` 호출)만 줄인다. 로컬(`local`)도 동일 — 클라이언트 사용자가 셸에서 실행하는 것과 같다. 보안 함의는 10.1.
- `exec` 결과 형식(7.2): 결과 줄 `<n> ok exec exit=0 ms=41 stdout=27B stderr=0B` 다음에 출력 내용이 두 칸 들여쓰기 + `<1|2>\t<line>`(1 = stdout, 2 = stderr) 형태로 한 줄씩 따라온다. `err`일 때도 출력 내용은 같은 방식으로 뒤따른다. `--jsonl`은 `exit`, `stdout`, `stderr`, `truncated` 필드(7.3).
- `exec`와 `open`은 둘 다 GUI 입력이 아니므로 전역 기본 `delay`를 적용하지 않는다(명시 `d=`는 적용; 비입력 커맨드 규칙 5.6과 일관, 확정). 결과가 화면에 나타나길 기다리려면 `open[wait=]`, `win[wait=]`, `sleep`, 또는 `open[d=1s]`.
- `exec`의 argv 분할 예: `exec[]git -C "C:\work dir" status` → `["git","-C","C:\work dir","status"]`. 파이프·리다이렉션·글롭·`~`가 필요하면 `exec[shell]`. 결과 줄의 `exit=` 값은 `noerr`일 때도 그대로 보고되므로 에이전트는 `exit=1`을 읽고 판단한다: `exec[noerr]grep -q TODO notes.txt` → `6 ok exec exit=1 ms=8 stdout=0B stderr=0B`.
- Windows에서 PowerShell이 필요하면 `exec[]powershell -NoProfile -Command <...>`처럼 직접 호출한다(`shell`은 항상 `cmd /C`). 출력 인코딩 처리는 `--help-windows`(부록 A.3).

### 5.5 캡처

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `cap` | `w` 현재 윈도우 또는 `disp=<n>` 디스플레이(둘 중 하나만, 4.5), `rect=x:y:w:h` 영역(논리 좌표, `:` 구분(4.2); `w`/`disp`와 결합 시 그 프레임 기준), `scale=<f>`(1.0 = 논리 좌표 1:1), `fmt=png\|jpg`(png; jpg는 M4), `q=<1-100>`(85, jpg만), `n=<int>`(1) 프레임 수, `ms=<dur>`(100) 프레임 간격, `label=<name>`(cap), `cursor` 포인터 합성 포함 (M4) | 저장 경로(클라이언트 로컬, 생략 시 자동) | 캡처를 **클라이언트 로컬 파일로 저장**하고 경로를 출력. `n>1`이면 `-00`, `-01` 접미. `cap[rect=0:0:600:400]`, `cap[w,scale=0.5]` |

- 자동 경로: `<out>/<NNNN>-<label>-<UTC timestamp>.<fmt>`, `<out>`은 `--out`, 없으면 `$GOTTO_HANDO_OUT`, 없으면 프로파일 `out`(8.4), 없으면 `$TMPDIR/gotto-hando/<profile>/<run-id>/`. 실행 시작 시 out 디렉터리를 첫 stdout 라인에 출력.
- 기본 `scale=1.0`은 "이미지 픽셀 = 논리 좌표"를 뜻한다. macOS Retina에서 원본은 2x이므로 기본은 **다운스케일**되어 저장된다. 원본 해상도가 필요하면 `scale=native`.
- 결과 라인에 `WxH`와 이미지 좌상단의 논리 좌표(`origin=x,y`)를 함께 출력하여, 영역/윈도우 캡처에서도 픽셀→절대 좌표 환산이 가능하게 한다.
- 커서는 기본적으로 캡처에 포함되지 않는다(OS 기본). `cursor` flag(M4)를 주면 포인터 이미지를 합성해 포함한다(확정).

### 5.6 흐름 제어·설정

| 커맨드 | 모디파이어 | payload | 동작 |
|---|---|---|---|
| `sleep` | — | `<dur>` | 대기(≤60s) |
| `set` | `delay=<dur>` 커맨드 후 기본 대기(조회·`sleep`·`set`·`cap`·`exec`·`open`은 기본값 적용 제외, 명시 `d=`는 적용, 6.1), `txtms=<dur>` `txt` 기본 간격, `keyms=<dur>` `k` 기본 간격 | (없음) | 이후 줄부터 적용되는 실행 상태 변경. `set[delay=200]` |
| `#` | — | 아무 텍스트 | 주석 |

### 5.7 조회(query)

| 커맨드 | 출력 |
|---|---|
| `qinfo` | `os=darwin osver=14.5 arch=arm64 ver=0.1.0 primary=cmd desktop=0,0 2560x1440 displays=2 session=active perms=accessibility:ok,screen:ok` |
| `qdisp` | 디스플레이당 한 줄: `  <idx>\t<x>,<y> <w>x<h>\tscale=<f>\t[primary]` |
| `qmouse` | `  <x>,<y>` |
| `qwin` / `qwin[]<selector>` | 윈도우당 한 줄: `  <id>\t<pid>\t<app>\t<x>,<y> <w>x<h>\t<flags>\t<title>` (flags: `*`focused, `min`, `hidden`) |
| `qclip` | 5.3 참조 |

조회 커맨드에는 전역 기본 `delay`를 적용하지 않는다. 단 `d=`를 명시하면 그 값은 적용된다(명시 > 기본; "전역 delay 제외" 규칙은 기본값에만 해당. 확정). `exec` 결과의 stdout/stderr 출력 형식은 5.4.

`qinfo` 필드 값: `perms=`는 macOS에서 `accessibility:ok|missing,screen:ok|missing`, Windows에서는 권한 개념이 없어 `n/a`. `session=`은 `active`(입력·캡처 가능) / `locked`(잠금 화면·스크린세이버·UAC 보안 데스크톱) / `inactive`(GUI 세션이 아님: macOS SSH 셸, Windows 세션 0·비활성 RDP 세션). Windows는 서버 토큰의 승격 여부 `elevated=0|1`을 덧붙인다(9.2). 값의 의미와 조치는 `--help-macos`/`--help-windows`(부록 A.2, A.3).

### 5.8 제한(limits)

| 항목 | 제한 |
|---|---|
| 줄 수 | 1000 |
| 한 줄 길이 | 64 KiB (`[f]` 파일 내용은 별도 64 KiB) |
| `k` payload 키 수 | 순차 64, 코드 8(모디파이어 flag 포함: `k[csam]a` = 5키) |
| `sleep` | ≤ 60s, `d=` ≤ 10s, `ms=` ≤ 10s, `wait=` ≤ 60s, `exec timeout=` ≤ 60s |
| `scroll` 틱 수 | 1..50 |
| `exec` 출력 | stdout/stderr 각 ≤ 64 KiB(초과분 절단, `truncated=1`) |
| `drag` | payload 점 1..200(1점이면 현재 위치가 시작점이 되어 경로는 2점), `steps` 1..200 |
| `label=` | `[A-Za-z0-9][A-Za-z0-9_-]{0,47}` (cua-batch와 동일, 경로 탈출 방지) |
| `cap n=` | ≤ 120, 실행당 캡처 총 ≤ 121 |
| 실행 전체 시간 | `--timeout`(300s). 요청의 `deadline_ms`로 서버에 전달되어 서버가 동일 값을 강제(6.3) |
| `scale` | 0.1..4.0 또는 `native` |

---

## 6. 실행 모델

### 6.1 단계

```
argv/stdin/file ─► lexer/parser ─► IR(ops) ─► static validate ─► transport ─► backend preflight ─► sequencer ─► results(stream) ─► stdout
                       │ exit 2                    │ exit 2             │ exit 3           │ exit 4              │ exit 1
```

1. **파싱**: 전 줄을 IR로. 문법 오류는 줄 번호·열·원문과 함께 보고.
2. **정적 검증**: 값 범위, held 균형(`ku`/`mu`가 선행 `kd`/`md`와 짝이 맞는지, 이미 held인 버튼으로 `c`/`drag` 금지), 프레임 모디파이어 중복·`%`+`r` 조합(4.5), 코드 8키(flag 포함), 파일(`[f]`) 존재·크기·UTF-8, 제한(5.8). `--check`/`--ir`는 이 단계까지 수행하고 종료(연결 안 함).
3. **연결**: `local`이면 in-process backend. 원격이면 프로파일 엔드포인트에 `GET /v1/info` → 실패 시 exit 3.
4. **preflight**(backend): 권한(접근성/화면 기록), 세션 상태(활성·잠금 해제), 물리적으로 눌린 키/버튼 없음, 좌표가 현재 데스크톱 안, 키 이름이 플랫폼에서 지원됨. 실패 시 아무 입력 없이 exit 4.
5. **순차 실행**: op 하나씩. 각 op 완료 후 `d=` 또는 전역 `delay`(기본 100ms) 대기. 조회/`sleep`/`set`/`cap`/`exec`/`open`은 전역 기본값을 적용하지 않지만 명시 `d=`는 적용한다(명시 > 기본).
6. **종료 처리**: 정상/실패/데드라인 초과(6.3)/시그널(SIGINT, 연결 끊김) 어느 경우든 held 키·버튼을 역순 해제. `--cap-on-error`면 실패 시 캡처 1장 추가.

### 6.2 상태 (한 실행 = 하나의 상태 머신)

| 상태 | 소유 | 초기값 | 변경 |
|---|---|---|---|
| 현재 윈도우 | engine | 없음(→ `w` 프레임은 OS 포커스 윈도우를 그때그때 조회) | `win` |
| held 키/버튼 | engine (backend에 위임 안 함) | 비어 있음 | `kd/ku/md/mu`, 모디파이어 flag는 명령 안에서만 |
| 포인터 위치 | OS | — | `m c md drag`; `qmouse`로 조회 |
| `delay`, `txtms`, `keyms` | engine | 100ms / 0 / 30ms (`delay` 초기값 우선순위: `--delay` > 프로파일 `delay` > 100ms. 기본 100ms 확정 — cua-batch의 330ms는 Blender 기준 보수치이며 앱별 조정은 프로파일 `delay`로) | `set`, `--delay`, 프로파일 `delay` |
| 캡처 카운터·out 디렉터리 | client | 0 / 자동 | `cap` |

실행 간에는 아무것도 유지되지 않는다(서버도 상태 없음). 다음 실행은 새 상태로 시작한다.

### 6.3 에러 정책

- **기본 fail-fast**: op 실패 → 이후 op 실행 안 함 → held 해제 → (옵션) 캡처 → exit 1. 실패한 op 이후의 줄은 결과에 `skip`으로 표기.
- `-k/--keep-going`: 실패해도 다음 줄 계속. 단 held 상태가 꼬이지 않도록 실패한 op가 잡은 키/버튼은 즉시 해제. 최종 exit는 1.
- 조회 커맨드의 "결과 없음"(`qwin` 0개)은 실패가 아니다. `win`의 "일치 없음"은 실패다.
- 입력 수락 ≠ 앱 반영. 결과 라인은 `ok`만 말하며, 실제 효과는 `cap`으로 확인해야 함을 --help에 명시.
- `exec`의 exit code ≠ 0은 기본적으로 `err`(E_EXEC)로 fail-fast 대상이다. 종료 코드가 0이 아닌 것이 정상인 명령(`grep`, `diff`)은 `exec[noerr]`로 실행하면 `ok`에 `exit=N`만 보고하고 계속한다(확정, 5.4). `-k`는 모든 줄에 적용되는 전역 완화이고 `noerr`는 그 줄만 완화한다. 타임아웃(E_TIMEOUT)은 `noerr`로 완화되지 않는다.
- **데드라인(확정)**: 클라이언트의 `--timeout`(기본 300s)은 요청에 `deadline_ms`로 실려 가고 서버가 **동일 값을 강제**한다(`local`은 engine이 in-process로 강제). 초과 시 서버는 진행 중인 op를 중단하고(보간 이동·버스트 캡처·`win[wait=]` 폴링·`exec` 자식 프로세스 kill 포함) held 키/버튼을 역순 해제한 뒤, 그 op를 `err ... (E_TIMEOUT)`, 나머지 줄을 `skip`으로 보고하고 `done`을 정상 출력한다 → exit 1. 즉 타임아웃은 **`state=unknown`이 아니라 정상 종료 경로**다. 클라이언트도 같은 값을 알고 있으므로 데드라인 + 유예(5s) 안에 `done`이 오지 않으면 연결 손실로 취급한다.
- **연결 손실**: 스트림이 `done` 없이 끊기거나 위 유예를 넘기면 클라이언트는 `done` 줄에 `state=unknown`을 표기하고 exit 5로 끝난다. 서버는 연결 끊김을 취소로 간주해 held 해제를 시도한다(8.3). 에이전트는 다음 실행 전에 `qinfo`/`cap`으로 상태를 점검해야 한다.

---

## 7. CLI 인터페이스와 출력 형식

### 7.1 호출 형태

```
gotto-hando <profile> [options] [line ...]
gotto-hando <profile> -f <file|->            # 파일 또는 stdin
gotto-hando <profile>                        # stdin이 파이프면 stdin에서 읽음, TTY면 qinfo 실행 (argv 줄이 있으면 stdin은 읽지 않음)
gotto-hando --serve <port> [--token <t>] [--detach]   # 서버 모드 (127.0.0.1만 바인드; --detach는 Windows 콘솔 분리, 9.2)
gotto-hando local|<profile> --request-perms  # macOS: 접근성/화면 기록 권한 프롬프트 유도 — <profile>이면 원격 Mac의 GUI 세션에 띄움 (8.3, 9.1, 부록 A.2)
gotto-hando --help | --help-macos | --help-windows | --help-remote | --version | --profiles
```

| 옵션 | 의미 |
|---|---|
| `-f, --file <path>` | 시퀀스 파일(`-` = stdin). **argv 줄과 혼용 금지**(함께 주면 exit 2; 결과의 줄 번호가 단일 소스를 가리키도록. 확정) |
| `-k, --keep-going` | fail-fast 해제 |
| `--delay <dur>` | 초기 전역 delay |
| `--out <dir>` | 캡처 저장 디렉터리 |
| `--jsonl` | 결과를 JSON Lines로 |
| `--check` | 파싱·정적 검증만(연결 안 함), exit 0/2 |
| `--ir` | IR JSON을 stdout에 출력하고 종료(연결 안 함) |
| `--cap-on-error` | 실패 시 캡처 1장 |
| `--timeout <dur>` | 실행 전체 시간 제한(300s). 요청의 `deadline_ms`로 서버에 전달되어 양쪽이 같은 값을 강제(6.3). 초과는 `E_TIMEOUT` err + exit 1이지 exit 5가 아님 |
| `--ping` | 연결·권한·세션만 확인(= `qinfo`) |
| `-q, --quiet` | ok 라인 생략, err/warn/조회/캡처 라인만 |
| `--help` | 풀 매뉴얼(부록 A.1). 사용법·문법·커맨드·caveat·예제. 토픽 인자 없음(3.1) |
| `--help-macos` | macOS 서명·TCC 권한·세션 가이드(부록 A.2). `perms=...:missing`·`E_PERMISSION`이 나오면 |
| `--help-windows` | Windows 세션·작업 스케줄러·SmartScreen·UAC·DPI·코드페이지 가이드(부록 A.3). `session=`이 `active`가 아니면 |
| `--help-remote` | 원격 PC 설정 가이드(부록 A.4). 서버 `--serve`/`--token`, 클라이언트 `ssh -L`/프로파일 |
| `--request-perms` | **macOS 전용**(신규). `local`과 `<profile>` 둘 다 허용. `local`: 자기 프로세스에서 `AXIsProcessTrustedWithOptions(prompt=true)`와 `CGRequestScreenCaptureAccess()`를 호출해 시스템 권한 프롬프트를 띄운 뒤 `qinfo`와 같은 한 줄(`perms=accessibility:ok\|missing,screen:ok\|missing`)을 출력. `<profile>`: 서버의 `POST /v1/request-perms`(8.3)를 호출하고, `--serve` 프로세스가 자기 프로세스에서 같은 API를 호출해 **원격 Mac의 GUI 세션에** 프롬프트를 띄운 뒤 같은 한 줄을 돌려준다 — 전제는 서버가 LaunchAgent 등 Aqua(GUI) 세션에서 실행 중일 것(SSH 세션에서 직접 띄운 서버는 프롬프트 없이 `missing`만). 둘 다 `ok`면 exit 0, 아니면 exit 4. 로컬도 GUI 세션(터미널 등)에서 실행해야 프롬프트가 뜬다 — SSH 셸에서는 프롬프트 없이 `missing`만 출력. Windows 로컬은 exit 2, Windows 서버에 보내면 `perms=n/a`로 no-op ok(exit 0). 어느 경로든 토글 클릭은 그 Mac 앞의 사람이 해야 하며, 화면 기록은 부여 후 **프로세스(서버) 재시작**이 필요하다(9.1) |

`--help` 계열은 `<profile>` 없이 단독으로 쓴다. `--request-perms`는 `local` 또는 `<profile>` 뒤에 붙인다.

옵션은 `-`로 시작하고 명령 줄은 항상 소문자 알파벳·`#`·공백 중 하나로 시작하므로 위치에 관계없이 구분된다. 프로파일 이름은 `[a-z0-9][a-z0-9_-]*`.

### 7.2 stdout 형식 (기본, 에이전트 파싱용)

한 op당 한 줄. 공백 구분 필드, 첫 두 필드는 고정.

```
out /var/folders/xx/T/gotto-hando/winbox/20260907T131500-a1b2/
1 ok win id=2314 app=Blender matched=1 0,25 1440x875 "Untitled - Blender"
2 ok k
3 ok txt chars=5
4 ok cap /var/folders/xx/T/gotto-hando/winbox/20260907T131500-a1b2/0000-cap-20260907T131502.114Z.png 1440x900 origin=0,0 scale=1
5 ok qwin
  2314	4120	Blender	0,25 1440x875	*	Untitled - Blender
  2290	612	Terminal	1440,25 1120x875		zsh — 120x40
6 ok exec exit=0 ms=41 stdout=34B stderr=0B
  1	C:\Users\me\scenes\untitled.blend
7 err c[w]1500,300: coordinates outside current window (E_BOUNDS)
8 skip cap
done ok=6 err=1 skip=1 elapsed=1875ms held_released=0
```

- `<n>`은 1부터 시작하는 **입력 줄 번호**(주석·빈 줄 포함해서 센 번호. 에이전트가 자기 입력과 대조하기 쉽다).
- 상태: `ok` `err` `skip` `warn`(ok이지만 주의: `matched=3`, held 자동 해제 등).
- 다중 라인 결과(조회, `exec` 출력)는 두 칸 들여쓰기 + TAB 구분. `exec`는 `  <1|2>\t<line>`(1 = stdout, 2 = stderr, 출력 순서는 발생 순서 best-effort) 형태이며 `err`(exit ≠ 0, 타임아웃)일 때도 뒤따른다(5.4).
- 에러는 `<원문>: <메시지> (<E_CODE>)`. 에러 코드: `E_SYNTAX E_VALIDATE E_CONNECT E_PERMISSION E_SESSION E_BOUNDS E_NOWINDOW E_INPUT E_CAPTURE E_CLIPBOARD E_EXEC E_TIMEOUT E_UNKNOWN`.
- 마지막 줄 `done ...`은 항상 출력. exit 5(연결 손실)로 끝나는 경우 `state=unknown`이 덧붙는다(6.3). `--timeout` 초과(`E_TIMEOUT`)는 정상 `done` 경로다.
- 경고·진단은 stderr. stdout에는 위 형식만.

### 7.3 `--jsonl`

op당 한 JSON 객체(한 줄). 첫 줄은 `{"event":"start","out":...,"profile":...,"target":{os,...}}`, 마지막은 `{"event":"done",...}`.

```json
{"line":4,"status":"ok","cmd":"cap","path":"/.../0000-cap-....png","w":1440,"h":875,"origin":[0,25],"scale":1,"t_ms":312}
{"line":6,"status":"ok","cmd":"exec","exit":0,"stdout":"C:\\Users\\me\\scenes\\untitled.blend\n","stderr":"","truncated":false,"t_ms":41}
{"line":7,"status":"err","cmd":"c","src":"c[w]1500,300","code":"E_BOUNDS","msg":"coordinates outside current window"}
```

### 7.4 exit code

| code | 의미 |
|---|---|
| 0 | 모든 op 성공 |
| 1 | 하나 이상의 op 런타임 실패(입력은 일부 실행됨) |
| 2 | 문법/정적 검증 오류(아무것도 실행 안 됨) |
| 3 | 프로파일/연결 오류(아무것도 실행 안 됨) |
| 4 | preflight 실패 — 권한/세션/절대 좌표 범위/키 지원(아무것도 실행 안 됨) |
| 5 | 상태 불명(연결 손실 중 발생, held 해제 미확인). `--timeout` 초과는 여기 해당하지 않음 — 서버가 데드라인을 강제해 `E_TIMEOUT` err + exit 1로 정상 종료(6.3) |

---

## 8. 아키텍처: IR + backend + transport

```
┌──────────── client process (gotto-hando <profile>) ────────────┐
│ cli ─► syntax.Parse ─► ir.Sequence ─► ir.Validate ─► engine.Run │
│                                                      │          │
│                    ┌─────────────────────────────────┤          │
│                    ▼ (profile=local)                 ▼ (remote) │
│              backend.Local                     transport.Client │
│              (darwin|windows)                    HTTP/NDJSON    │
└──────────────────────────────────────────────────────┼──────────┘
                                          ssh -L tunnel │ 127.0.0.1:<port>
┌──────────── server process (gotto-hando --serve) ────▼──────────┐
│ transport.Server ─► engine.Run ─► backend.Local (darwin|windows)│
└─────────────────────────────────────────────────────────────────┘
```

### 8.1 파서 → IR

- IR은 **타입이 있는 op 리스트**로, JSON 직렬화 가능(`--ir`로 확인). cua-batch의 JSON 액션 union이 IR의 원형이며, 새 문법은 그 위의 표기법일 뿐이다.
- 각 op는 `line`(줄 번호)과 `src`(원문)를 보존해 에러 보고에 쓴다.
- 모디파이어 키 `p`(primary)는 IR에 **심볼 그대로**(`"primary"`) 남기고 backend가 OS에 맞춰 해석한다. 클라이언트가 연결 전에 IR을 만들 수 있게 하기 위함이다.
- `[f]` 파일은 파서 단계에서 읽어 IR에 인라인 텍스트로 넣는다(cua-batch `prepare`와 동일). 서버 프로세스 자체는 시퀀스 처리를 위해 파일 시스템을 읽거나 쓰지 않는다(로그 제외, 10.1). `exec`/`open`이 띄우는 자식 프로세스는 이 원칙 밖이다 — 그것은 사용자의 명령이다.
- `exec` op는 `{"op":"exec","argv":[...],"shell":false,"noerr":false,"timeout_ms":10000}`(직접 실행) 또는 `{"op":"exec","shell":true,"cmd":"...","noerr":false,"timeout_ms":...}`로, `open`은 `{"op":"open","target":"...","wait_ms":0}`로 IR에 들어간다. argv 분할은 클라이언트 파서가 한다(플랫폼 무관 규칙, 5.4). `noerr`는 engine이 결과 상태를 정할 때만 본다(backend는 exit code를 그대로 돌려줌).
- 캡처 op의 저장 경로와 `qclip[f]`의 저장 경로는 IR에 넣지 않는다(클라이언트 관심사). 캡처 IR에는 `label`, 프레임, 스케일만.
- 스키마 버전 `v`를 두어 클라이언트/서버 불일치를 감지한다.

### 8.2 backend 인터페이스 (Go)

```go
type Backend interface {
    Info(ctx) (Info, error)                       // os, displays, primary, perms, session
    Preflight(ctx, seq *ir.Sequence) error        // 권한·세션·held·범위·키 지원
    KeyDown/KeyUp(ctx, key ir.Key) error
    TypeText(ctx, s string, interval time.Duration) error
    MouseMove(ctx, p Point, dur time.Duration) error
    ButtonDown/ButtonUp(ctx, b ir.Button) error
    Scroll(ctx, dir ir.Dir, n int, by ir.ScrollUnit) error
    Windows(ctx, sel ir.Selector) ([]Window, error)
    Focus(ctx, w Window) error
    Capture(ctx, req ir.CaptureReq) (Image, error) // 인코딩 전 픽셀 + origin/scale
    ClipboardGet/ClipboardSet(ctx, s string) error
    MousePos(ctx) (Point, error)
    Open(ctx, target string) error                 // open -a / ShellExecute
    Exec(ctx, req ir.ExecReq) (ExecResult, error)  // os/exec 공통 구현. ctx 데드라인(--timeout, exec timeout=) 초과 시 kill. stdout/stderr 64 KiB 캡.
                                                   // shell=true: darwin `$SHELL -lc`(폴백 /bin/zsh -lc), windows `cmd /C`. windows는 콘솔 출력 코드페이지→UTF-8 변환(5.4)
}
```

- `click`, `drag`, `key` 코드, 반복, 보간, held 추적, 지연은 **engine**이 backend 원시 호출을 조합해 구현한다(cua-batch에서 engine/drag가 하던 역할). backend는 가능한 한 얇게.
- 플랫폼 구현은 빌드 태그(`//go:build darwin`, `//go:build windows`)로 분리. 테스트용 `fake` backend는 이벤트를 기록해 engine 계약 테스트(cua-batch `Fake` 이식)에 쓴다.

### 8.3 transport

| 모드 | 구현 |
|---|---|
| `local` | in-process: engine이 backend.Local을 직접 호출. 네트워크 없음 |
| remote | 클라이언트는 프로파일의 `host:port`로 HTTP/1.1. 서버는 동일 바이너리의 `--serve` |

프로토콜(HTTP + JSON, NDJSON 스트리밍):

| 엔드포인트 | 용도 |
|---|---|
| `GET /v1/info` | `Info` JSON. 버전·IR 스키마 버전 협상 |
| `POST /v1/run` | body = `{"deadline_ms": N, "ir": {...}}` — IR JSON과, 클라이언트 `--timeout`에서 계산한 남은 시간. **서버는 `deadline_ms`를 강제**한다(초과 시 진행 중 op 중단, held 해제, `E_TIMEOUT` err 후 정상 `done`; 6.3). 응답은 `application/x-ndjson`으로 **op 결과를 완료 즉시 스트리밍**. 서버는 동시에 하나의 run만 허용(409 busy) |
| `POST /v1/cancel` | 진행 중 run 취소(held 해제 포함). 클라이언트 SIGINT 시 호출. 연결 끊김도 취소로 간주 |
| `POST /v1/request-perms` | 클라이언트 `<profile> --request-perms`(7.1)가 호출. 서버가 **자기 프로세스에서** `AXIsProcessTrustedWithOptions(prompt=true)`·`CGRequestScreenCaptureAccess()`를 호출해 서버가 속한 GUI 세션에 시스템 프롬프트를 띄우고 `qinfo`와 같은 `perms` 문자열을 JSON으로 반환. 서버가 LaunchAgent 등 Aqua 세션에서 실행 중일 때만 프롬프트가 뜨며, SSH 셸에서 띄운 서버는 `missing`만 돌려준다. Windows 서버는 `perms=n/a`로 no-op ok. run 진행 중이면 409 busy |

캡처 이미지 반환 방식 결정: **NDJSON 이벤트에 base64로 인라인** → 클라이언트가 디코드해 로컬 파일로 저장하고 경로를 출력.

- 근거: 에이전트는 클라이언트 측 파일을 읽어야 한다(원격 경로는 무의미). 별도 blob 엔드포인트나 SFTP는 왕복·상태 관리를 늘린다. 단일 연결·단일 스트림이면 취소/오류 처리가 단순하다. 1440x900 PNG는 수백 KB~수 MB로 localhost 터널에서 base64 33% 오버헤드는 무시 가능하다.
- 서버는 이미지를 디스크에 남기지 않는다(cua-batch의 "원격 증거 폴더" 정책은 폐기; 에이전트 프롬프트에 포함된 텍스트가 원격에 남지 않는 것이 보안상 낫다).
- `exec`의 stdout/stderr도 같은 NDJSON 이벤트에 인라인(각 ≤ 64 KiB, UTF-8 문자열)한다. 절단 여부는 `truncated`.
- 큰 프레임 버스트(`n=120`)가 문제가 되면 이벤트 청킹 또는 `GET /v1/blob/<id>` 추가 검토(open question).

인증: 프로파일에 `token`이 있으면 `Authorization: Bearer`. SSH 터널이 전제이므로 선택이지만, 서버 호스트가 다중 사용자면 필수로 권고.

### 8.4 PC 프로파일

- 위치: macOS/Linux `~/.config/gotto-hando/profiles.toml`(`$XDG_CONFIG_HOME` 존중), Windows `%APPDATA%\gotto-hando\profiles.toml`. `--profiles`로 경로와 목록 출력.
- `local`은 예약어이며 파일에 정의할 수 없다.

```toml
[profiles.winbox]
host  = "127.0.0.1"      # 항상 localhost 터널 엔드포인트여야 함
port  = 47311
token = "s3cr3t"         # 선택
delay = "150ms"          # 선택: 이 PC의 기본 delay
out   = "~/shots/winbox" # 선택: 기본 캡처 디렉터리

[profiles.macmini]
host = "127.0.0.1"
port = 47312
```

`host`가 loopback(`127.0.0.1`, `::1`, `localhost`)이 아니면 **거부**한다(exit 3, `E_CONNECT`, 메시지에 "SSH 터널을 쓰라"는 안내; 확정). 서버에 `--bind`가 없으므로 loopback이 아닌 host는 어차피 연결될 수 없고, 클라이언트가 먼저 거부해 원인을 명확히 한다(10.1). 내부 VPN에서 터널 없이 쓰는 구성은 v1에서 지원하지 않는다.

---

## 9. 플랫폼별 구현 노트

### 9.1 macOS (darwin, cgo)

- **입력**: `CGEventPost(kCGHIDEventTap, ev)`. 키는 가상 키코드(`kVK_*`, US 레이아웃 위치 기준). 모디파이어는 별도 keydown 이벤트 + 이후 이벤트의 `CGEventFlags`에도 설정(앱에 따라 flags만 보는 경우가 있음). 텍스트는 `CGEventKeyboardSetUnicodeString`로 keydown/keyup 쌍 — 대부분의 앱에서 IME를 우회해 한글/이모지 삽입 가능. 마우스 클릭은 `CGEventSetIntegerValueField(kCGMouseEventClickState, n)`로 더블클릭 상태를 명시해야 한다. 스크롤은 `CGEventCreateScrollWheelEvent(kCGScrollEventUnitLine)`.
- **좌표**: CGEvent/AX는 좌상단 원점, `NSScreen`은 좌하단 원점 — 구현 시 혼동 주의. 주 디스플레이 좌상단 = (0,0).
- **윈도우**: 목록은 `CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|ExcludeDesktopElements)`(id, owner pid/name, bounds, layer 0만). 포커스는 `NSRunningApplication.activate` + `AXUIElement`로 해당 윈도우를 `kAXMainAttribute=true`, `AXRaise`. 최소화 해제는 `kAXMinimizedAttribute=false`.
- **캡처**: macOS 12.3+는 `ScreenCaptureKit`(`SCScreenshotManager`, 14+)이 정식. 폴백은 `CGWindowListCreateImage`(14에서 deprecated, 아직 동작). 최후 수단 `/usr/sbin/screencapture -x [-R x,y,w,h | -l <wid>] path`. Retina 원본은 2x 픽셀이므로 기본 `scale=1`은 다운샘플.
- **권한(TCC)**: Accessibility(입력 이벤트 post, AX 윈도우 제어), Screen Recording(캡처; 없으면 윈도우 타이틀도 빈 문자열로 옴). `qinfo`는 `AXIsProcessTrusted()`·`CGPreflightScreenCaptureAccess()`로 `perms=accessibility:ok|missing,screen:ok|missing`을 보고한다(프롬프트 없음). `--request-perms`(7.1)는 `AXIsProcessTrustedWithOptions(prompt=true)`·`CGRequestScreenCaptureAccess()`로 시스템 프롬프트를 띄운다. 프롬프트는 바이너리를 목록에 **추가만** 하고 토글은 사람이 켜야 하며, 화면 기록은 부여 후 프로세스를 재시작해야 반영된다. `tccutil reset Accessibility|ScreenCapture <bundle-id>`는 초기화만 가능하고 부여는 못 한다. MDM(PPPC 프로파일)으로 접근성은 사전 허용 가능하나 화면 기록은 불가.
- **서명과 identity(사실 정리)**: Go 링커는 darwin 바이너리를 자동 ad-hoc 서명하므로 로컬 빌드·실행에는 추가 서명이 필요 없다. `go build`/`go install`/`git clone` 후 빌드한 바이너리에는 quarantine 속성이 없다. 브라우저로 내려받은 바이너리는 quarantine으로 차단되며 `xattr -d com.apple.quarantine <bin>`으로 해제한다. 공증(notarization)은 Apple Developer Program 유료 가입이 필요하며 이 도구에는 불필요하다. **TCC 권한은 코드 서명 identity에 묶인다**: ad-hoc 서명은 designated requirement가 cdhash라 빌드마다 바뀌고, 재빌드하면 권한을 다시 부여해야 한다(`--identifier`를 고정해도 마찬가지). 해결은 키체인 접근에서 "코드 서명" 용도의 자체 서명 인증서(예: `gotto-hando-dev`)를 만들고 빌드 후 `codesign -s "gotto-hando-dev" --force ./gotto-hando`로 서명하는 것이다(인증서 생성: 키체인 접근 → 인증서 지원 → 인증서 생성 → 이름 입력, 유형 "코드 서명"). 이러면 identity가 인증서 기준이 되어 재빌드해도 권한이 유지된다. Makefile/`scripts/`에서 자동화한다(12장).
- **책임 프로세스(responsible process) 규칙**: 터미널 앱(Terminal, iTerm2, VS Code 통합 터미널 등)에서 실행하면 TCC는 권한을 **터미널 앱에 귀속**시킨다. 따라서 로컬 사용(`gotto-hando local`)은 터미널 앱에 한 번 권한을 주면 재빌드와 무관하게 유지되고, 위 서명 문제는 실제로는 **`--serve`를 launchd(LaunchAgent)나 SSH로 띄우는 원격 Mac**에서만 부각된다(그때는 책임 프로세스가 바이너리 자신).
- **원격 Mac의 권한 부여**: SSH 세션에서는 권한 프롬프트가 뜨지 않는다(SSH 셸에서 실행한 `--request-perms`도 `missing`만 출력). 대신 `gotto-hando <profile> --request-perms`는 서버의 `POST /v1/request-perms`(8.3)를 호출하고, `--serve` 프로세스가 자기 프로세스에서 `AXIsProcessTrustedWithOptions(prompt=true)`·`CGRequestScreenCaptureAccess()`를 호출해 **원격 Mac의 GUI 세션에** 프롬프트를 띄운다. 전제는 서버가 LaunchAgent 등 Aqua(GUI) 세션에서 실행 중일 것 — SSH 세션에서 직접 띄운 서버는 프롬프트가 뜨지 않는다. 프롬프트는 바이너리를 목록에 추가할 뿐이므로 토글은 여전히 그 Mac 앞에서 또는 화면 공유로 시스템 설정 → 개인정보 보호 및 보안 → 손쉬운 사용 / 화면 기록에서 사람이 켜야 하며(항목이 없으면 `+`로 수동 추가), 한 머신·한 identity당 한 번이다. 화면 기록은 서버 재시작(`launchctl kickstart -k`) 후 반영. 에이전트가 할 수 있는 것은 `qinfo`로 누락을 확인하고 `<profile> --request-perms`를 시도한 뒤 사용자에게 설정 경로를 안내하는 것까지다(사람의 토글 클릭은 자동화 불가). 사용자용 절차는 부록 A.2(`--help-macos`).
- **GUI 세션**: SSH 로그인 셸에서 띄운 프로세스는 Aqua 세션 bootstrap namespace가 아니어서 CGEvent가 무시되거나 캡처가 빈 이미지가 된다. `--serve`는 GUI 로그인 세션에서(Terminal.app, 또는 `~/Library/LaunchAgents/*.plist`) 실행해야 하며 `qinfo`의 `session=` 필드로 검출한다. 화면 잠금·스크린세이버 상태는 `session=locked`로 보고하고 preflight 실패. 잠금 방지는 `caffeinate -d` 또는 시스템 설정의 잠금 화면 설정.
- **Secure Input**: 비밀번호 필드 등 Secure Event Input 활성 시 키 이벤트가 차단됨 — `E_INPUT`에 힌트 포함.
- **Retina·다중 디스플레이**: 좌표는 points(4.5)이고 기본 캡처는 points와 1:1로 다운스케일된다(5.5). 디스플레이별 backing scale이 다를 수 있으므로(`qdisp`의 `scale=`) `scale=native` 캡처의 픽셀 좌표는 그대로 입력에 쓸 수 없다. 주 디스플레이 좌상단이 (0,0)이고 왼쪽/위쪽 디스플레이는 음수 좌표.

### 9.2 Windows (cgo 없음, `golang.org/x/sys/windows`)

- **DPI**: 프로세스 시작 시 `SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)`(매니페스트 병행). 좌표·캡처 모두 물리 픽셀.
- **입력**: `SendInput`. cua-batch `native.py` 규칙 그대로: 키패드는 `KEYEVENTF_SCANCODE`로 물리 위치 전송, 화살표/Ins/Del/Home/End/PgUp/PgDn/Win/우측 Ctrl·Alt/NumEnter는 `EXTENDEDKEY`; 모든 키에 `MapVirtualKeyW` 스캔코드 동봉(일부 게임/DirectInput 앱이 스캔코드만 봄). 텍스트는 `KEYEVENTF_UNICODE`(UTF-16 단위, 서로게이트 쌍 순서 유지). 마우스 이동은 가상 화면 기준 0..65535 정규화 + `MOUSEEVENTF_VIRTUALDESK`. 여러 이벤트를 하나의 `SendInput` 배열로 보내면 원자성이 좋아지나 `ms=` 간격이 필요하면 개별 호출.
- **포커스**: `SetForegroundWindow`는 호출 프로세스가 전면이 아니면 거부되고 작업표시줄만 깜빡인다. 대책 순서: `AllowSetForegroundWindow`가 불가하므로 (1) `AttachThreadInput(cur, target, TRUE)` 후 `SetForegroundWindow`+`BringWindowToTop`, (2) 그래도 실패 시 합성 Alt 키 탭 후 재시도. `ShowWindow(SW_RESTORE)`로 최소화 해제. 결과는 `GetForegroundWindow`로 검증 후 실패면 `E_NOWINDOW`.
- **윈도우 목록**: `EnumWindows` + `IsWindowVisible` + `DWMWA_CLOAKED` 제외 + 타이틀 비어있지 않음 + 툴 윈도우 제외. 프레임은 `DwmGetWindowAttribute(DWMWA_EXTENDED_FRAME_BOUNDS)`(그림자 제외).
- **캡처**: GDI `BitBlt(CAPTUREBLT)` from `GetDC(NULL)` 가상 화면 좌표. 윈도우는 `PrintWindow(PW_RENDERFULLCONTENT)`(가려져 있어도 가능, 일부 GPU 앱은 검정). 옵션으로 DXGI Desktop Duplication(고속, 보호 콘텐츠·세션 전환 시 재초기화 필요).
- **세션**: `ProcessIdToSessionId(self) == WTSGetActiveConsoleSessionId()` 이고 `OpenInputDesktop` 이름이 `Default`여야 입력 가능. 잠금 화면(`Winlogon`), UAC 보안 데스크톱, 로그오프, RDP 연결 해제 후 잠김 상태에서는 실패 → `E_SESSION`. 세션 0 서비스에서는 절대 불가하므로 `--serve`는 서비스가 아니라 로그인 사용자 세션의 일반 프로세스(시작 프로그램/작업 스케줄러 "사용자가 로그온할 때만")로 띄운다.
- **RDP caveat**: RDP로 접속해 있으면 콘솔 세션이 아니라 RDP 세션이 활성이며, 연결을 끊으면 세션이 잠긴다. 원격 데스크톱 없이 입력을 살리려면 `tscon %SESSIONNAME% /dest:console`로 콘솔로 돌려보내고, 자동 잠금을 해제(그룹 정책/`SetThreadExecutionState`)한다. 헤드리스 머신은 더미 HDMI 플러그가 필요할 수 있다.
- **콘솔**: `--serve`는 `-H windowsgui` 링크 플래그로 빌드된 별도 바이너리가 아니라, 같은 바이너리를 `--serve --detach`로 띄우면 자기 콘솔을 `FreeConsole`(cua-batch worker와 동일)하고 로그를 파일(`%LOCALAPPDATA%\gotto-hando\serve.log`)로 보낸다.
- **작업 스케줄러 상주**: 트리거 "로그온할 때"(해당 사용자), 보안 옵션 "사용자가 로그온한 경우에만 실행"(이래야 대화형 데스크톱을 가진다; "로그온 여부에 관계없이"는 세션 0 상당이라 불가), "가장 높은 수준의 권한으로 실행"은 UIPI 대응이 필요할 때만. 조건 탭의 "AC 전원일 때만"·설정 탭의 "다음 시간 이상 실행되면 중지"는 해제. `schtasks` 예시는 부록 A.3.
- **UAC/UIPI**: UAC 승격 창은 보안 데스크톱(`Winlogon`)에서 뜨므로 그동안 입력·캡처가 실패한다(`E_SESSION`). 관리자 권한으로 실행 중인 앱은 비승격 프로세스의 `SendInput`을 UIPI로 조용히 무시한다 — `GetLastError`로 구분되지 않으므로 서버가 감지하지 못하며, 대책은 서버를 관리자로 실행하는 것뿐(작업 스케줄러 "가장 높은 수준의 권한"). `qinfo`는 자기 토큰의 승격 여부를 `elevated=0|1`로 보고한다.
- **SmartScreen/Defender**: 브라우저로 내려받은 exe는 Mark-of-the-Web 때문에 첫 실행 시 SmartScreen이 막는다("추가 정보" → "실행", 또는 PowerShell `Unblock-File`). `go build`한 바이너리에는 MotW가 없다. 서명 없는 Go 바이너리를 Defender가 오탐하는 경우가 있어 폴더 제외 등록이 필요할 수 있다. 사람의 클릭이 필요한 단계는 자동화 불가.
- **`exec` 인코딩**: `cmd /C`의 출력은 OEM 코드페이지(CP949, CP437 등)일 수 있다. 서버는 `GetConsoleOutputCP()`(콘솔이 없는 `--detach` 상태면 실패 → `GetACP()`)로 코드페이지를 얻어 `MultiByteToWideChar`로 UTF-8 변환, 디코드 불가 바이트는 U+FFFD(5.4, 확정). `chcp 65001`을 강제하지 않는다(프로그램마다 무시하거나 깨지므로). 명령 자체가 UTF-8을 낸다고 알면 `exec[shell]chcp 65001 >nul && <cmd>`로 콘솔 코드페이지를 바꿀 수 있다.
- **물리 입력 검사**: preflight에서 `GetAsyncKeyState`로 모디파이어/마우스 버튼이 실제로 눌려 있으면 거부.

### 9.3 공통

- 키 이름은 물리 키 기준이므로 비-US 레이아웃에서 `k[]a`가 다른 문자를 낼 수 있다. 문자 입력은 `txt`.
- IME(한글 등) 활성 상태에서 `k` 알파벳은 IME 조합에 들어간다. `txt`는 대체로 우회하지만 앱에 따라 다르므로 캡처로 확인.
- 유니코드 텍스트 주입이 무시되는 앱(Blender, 일부 게임, Win32 콘솔): `k`(개별 키) 또는 `paste`로 대체.

---

## 10. 원격 실행과 보안

### 10.1 원칙

1. `--serve`는 **`127.0.0.1`에만 바인드**한다. `--bind` 옵션을 제공하지 않는다(v1). 네트워크 노출은 오직 SSH 터널을 통해서만. 대칭으로 클라이언트 프로파일의 `host`도 loopback만 허용하고 그 외는 거부한다(8.4, 확정).
2. 서버는 인증된 SSH 사용자(=로컬 사용자)만 접근 가능하다고 가정한다. 서버 호스트의 다른 로컬 사용자가 데스크톱을 제어할 수 있으므로 공유 호스트에서는 `--token` 필수.
3. 서버 프로세스 자체는 파일 시스템에 쓰지 않는다(로그 제외). 시퀀스 텍스트·캡처·`exec` 출력은 메모리에서만 처리하고 응답 후 폐기. `exec`/`open`이 띄운 자식 프로세스가 하는 일은 사용자의 명령이며 이 원칙의 대상이 아니다.
4. 서버는 동시에 하나의 run만 실행한다. 클라이언트 연결이 끊기면 run을 취소하고 held 입력을 해제한다(`exec` 자식 프로세스도 kill). 서버는 요청에 실린 `deadline_ms`를 강제한다(6.3).
5. **gotto-hando 서버 = 그 계정의 셸과 동등한 권한**(확정). `exec`(임의 명령)과 `open`(임의 경로)을 제공하므로 서버 포트에 도달할 수 있는 주체는 서버 계정으로 무엇이든 실행할 수 있다. 이것이 새 권한을 추가하지는 않는다 — 원격 사용의 전제가 SSH 터널이고, 그 SSH 계정은 이미 같은 셸을 가진다(5.4). 그러나 바로 이 때문에 1번(127.0.0.1 전용, `--bind` 없음)과 2번(공유 호스트 `--token`)이 더 중요하다: **서버 포트 노출 = 원격 셸 노출**이다. allowlist는 두지 않는다(SSH 셸에 allowlist가 없는 것과 같은 이유).

### 10.2 설정 절차 (`--help-remote` 내용; 최종 텍스트는 부록 A.4)

플랫폼 고유 단계(macOS 권한·서명·LaunchAgent, Windows 세션·작업 스케줄러)는 여기서 반복하지 않고 `--help-macos`(A.2)·`--help-windows`(A.3)를 가리킨다.

**서버 측(제어 대상 PC)**

1. 바이너리 배치: `gotto-hando`를 PATH에. 버전 확인 `gotto-hando --version`.
2. 플랫폼 준비: macOS는 서명 identity 고정 + 접근성/화면 기록 권한(`--help-macos`), Windows는 대화형 로그온 세션 + 필요 시 승격(`--help-windows`). 확인은 양쪽 모두 `gotto-hando local --ping`으로 `perms=`/`session=`.
3. GUI 세션에서 서버 실행: `gotto-hando --serve 47311 --token <t>`. 상주 방법(LaunchAgent plist / 작업 스케줄러)은 각 플랫폼 도움말.
4. sshd 활성화(macOS 원격 로그인 / Windows OpenSSH Server). 키 기반 인증 권장.
5. 로컬 확인: `gotto-hando local --ping` 및 `curl -s -H 'Authorization: Bearer <t>' 127.0.0.1:47311/v1/info`.

**클라이언트 측(에이전트가 도는 PC)**

1. 터널: `ssh -N -f -L 47311:127.0.0.1:47311 user@remote` (`-o ServerAliveInterval=30` 권장. autossh 가능).
2. 프로파일 등록: `~/.config/gotto-hando/profiles.toml`에 `[profiles.winbox] host="127.0.0.1" port=47311 token="<t>"`.
3. 확인: `gotto-hando winbox --ping` → `os=windows ... session=active`. `session=locked`면 서버 측 데스크톱 잠금 해제.
4. 첫 캡처: `gotto-hando winbox cap` 후 이미지 확인, 좌표 선택.

### 10.3 세션 caveat 요약 (도움말 분배: `win`/`mac`/`remote`/`help` 열)

| 상황 | 증상 | 대책 | 도움말 |
|---|---|---|---|
| Windows RDP 연결 끊김 | 세션 잠김, `E_SESSION` | `tscon ... /dest:console`, 자동 잠금 해제 | win |
| Windows 로그오프/잠금 화면 | 입력·캡처 실패 | 잠금 해제, autologon | win |
| Windows UAC 프롬프트 | 보안 데스크톱, 입력 불가 | 프롬프트 처리 후 재시도 | win |
| Windows 관리자 권한 앱 | 입력이 조용히 무시됨(UIPI) | 서버를 관리자로 실행 | win |
| Windows 서비스(세션 0)에서 서버 실행 | 항상 실패 | 작업 스케줄러 "사용자가 로그온한 경우에만" | win |
| Windows SmartScreen/Defender | 첫 실행 차단·오탐 | "실행"/`Unblock-File`/제외 등록(사람) | win |
| macOS SSH 셸에서 `--serve` 실행 | 이벤트 무시/빈 캡처 | GUI 세션(Terminal/LaunchAgent)에서 실행 | mac |
| macOS 화면 잠금/스크린세이버 | `session=locked` | 잠금 해제(`caffeinate -d` 등) | mac |
| macOS 재빌드 후 권한 소실 | `perms=accessibility:missing` | 자체 서명 인증서로 `codesign`, 재부여(9.1) | mac |
| macOS 원격에서 권한 프롬프트 안 뜸 | `<profile> --request-perms`가 프롬프트 없이 `missing` | 서버를 GUI 세션(LaunchAgent)에서 재시작 후 재시도, 또는 그 Mac 앞/화면 공유로 수동 추가 | mac |
| macOS 브라우저 다운로드 바이너리 | 실행 차단(quarantine) | `xattr -d com.apple.quarantine` | mac |
| 헤드리스 Windows | 디스플레이 없음 → 캡처 실패 | 더미 디스플레이 플러그 | win |
| 물리 키/버튼이 눌려 있음 | preflight 실패 | 물리 입력 해제 | help |
| macOS LaunchAgent로 띄운 서버에서 `exec` 명령을 못 찾음 | `E_EXEC` (launchd의 PATH 축소; Windows 작업 스케줄러는 보통 사용자 PATH를 유지) | `exec[shell]`(로그인 셸이 PATH 복구) 또는 절대 경로 | mac |
| 프로파일 `host`가 loopback이 아님 | exit 3 | SSH 터널로 바꾸고 `host="127.0.0.1"` | remote |

---

## 11. cua-batch ↔ gotto-hando 매핑

| cua-batch JSON | gotto-hando 한 줄 | 비고 |
|---|---|---|
| `{"type":"click","x":1,"y":2,"button":"right"}` | `c[b=right]1,2` | |
| `{"type":"double_click","x":1,"y":2}` | `c[n=2]1,2` | |
| `{"type":"mouse_move","x":500,"y":400}` | `m[]500,400` | |
| `{"type":"mouse_down","button":"middle"}` | `md[b=middle]` | |
| `{"type":"mouse_up","button":"middle"}` | `mu[b=middle]` | |
| `{"type":"drag","path":[[0,0],[10,20],[20,0]],"duration":.4,"steps":2,"button":"middle"}` | `drag[b=middle,ms=400,steps=2]0,0 10,20 20,0` | |
| `{"type":"scroll","direction":"down","amount":2,"by":"page","x":..,"y":..}` | `m[]x,y` + `scroll[by=page]down 2` | 위치는 별도 이동. 틱 수는 payload로만 |
| `{"type":"key","keys":["ctrl","shift","a"]}` | `k[cs]a` 또는 `k[]ctrl+shift+a` | |
| `{"type":"key","keys":["shift"]}` (단독 modifier) | `k[]shift` | |
| `{"type":"text","text":"hello"}` | `txt[]hello` | |
| `{"type":"paste","text":"code\nsecond"}` | `paste[]code\nsecond` | 이스케이프 |
| `{"type":"paste_file","path":"x.py"}` | `paste[f]x.py` | |
| `{"type":"wait","seconds":0.2}` | `sleep[]200` | ms 기본 |
| `{"type":"screenshot","label":"after"}` | `cap[label=after]` | |
| `{"type":"capture_frames","count":3,"interval_ms":100,"label":"view"}` | `cap[n=3,ms=100,label=view]` | |
| `"action_delay":0.1` / per-action `"delay":0` | `--delay 100` / `set[delay=100]` / `k[d=0]x` | |
| `capture` 서브커맨드 | `gotto-hando <p> cap` | |
| `--validate` | `--check` | |
| `--summary` | `-q` | |
| 최종 자동 캡처 | 명시적 `cap` 또는 `--cap-on-error` | 암묵 캡처 제거 |
| (없음) | `win`, `qwin`, `[w]` 좌표, `cap[w]`, `cap[rect=]`, `scale=`, `clip`, `qclip`, `qinfo`, `qdisp`, `qmouse`, `kd/ku` | 신규 |
| (없음) | `exec` | 신규(cua-batch에 없음). 타깃에서 명령 실행, stdout/stderr/exit code 회수 |
| (없음) | `open` | 신규(cua-batch에 없음). 앱/경로 실행. cua-batch의 SSH+PowerShell 부트스트랩·Cua `launch_app`은 내부 구현이지 사용자 기능이 아님 |
| `CUA_SSH_HOST`, `CUA_REMOTE_PYTHON`, `CUA_DRIVER` | `profiles.toml` | |
| `CUA_OUTPUT_DIR` | `--out` / `$GOTTO_HANDO_OUT` / 프로파일 `out` | |
| 원격 임시 폴더 + scp | NDJSON 스트림 + 클라이언트 로컬 저장 | |

---

## 12. 리포 구조 초안

```
gotto-hando/
├── go.mod                      # module github.com/kang-sw/gotto-hando
├── CONCEPT.md                  # 이 문서
├── cmd/gotto-hando/main.go     # 진입점: 옵션 파싱, --serve/--help 분기
├── internal/
│   ├── syntax/                 # lexer.go parser.go escape.go coord.go duration.go selector.go + golden tests
│   ├── ir/                     # ops.go (타입), json.go, validate.go, limits.go, version.go
│   ├── engine/                 # run.go (시퀀서), held.go, drag.go, timing.go, fake_backend_test.go
│   ├── backend/
│   │   ├── backend.go          # Backend 인터페이스, Info/Window/Image/ExecResult 타입, 에러 코드
│   │   ├── exec.go             # exec 공통 구현(os/exec, 64 KiB 출력 캡, ctx 데드라인 kill); exec_darwin.go(로그인 셸), exec_windows.go(cmd /C, 코드페이지→UTF-8)
│   │   ├── darwin/             # cgo: input.go(CGEvent) window.go(AX/CGWindowList) capture.go(SCK/CG) clipboard.go session.go open.go
│   │   ├── windows/            # sendinput.go window.go capture_gdi.go clipboard.go session.go dpi.go open.go(ShellExecute)
│   │   └── fake/               # 테스트용 이벤트 기록 backend
│   ├── transport/
│   │   ├── proto.go            # 요청/이벤트 JSON 타입, NDJSON 인코딩
│   │   ├── client.go           # HTTP 클라이언트, 스트림 소비, 캡처 로컬 저장
│   │   ├── server.go           # --serve: 127.0.0.1 바인드, 단일 run 뮤텍스, 취소
│   │   └── local.go            # in-process 연결
│   ├── profile/                # profiles.toml 로딩, local 예약, 경로 규칙
│   ├── output/                 # plain/jsonl 라이터, exit code 계산
│   ├── capture/                # 픽셀 → PNG/JPEG 인코딩, 스케일링
│   ├── help/                   # help.txt help-macos.txt help-windows.txt help-remote.txt (embed; 부록 A.1~A.4)
│   └── perms/                  # --request-perms (darwin: AXIsProcessTrustedWithOptions, CGRequestScreenCaptureAccess; 그 외 exit 2)
├── docs/                       # help 텍스트의 소스가 되는 마크다운(선택), 설계 노트
├── testdata/                   # 문법 골든 파일, IR 스냅샷
├── scripts/                    # 크로스 빌드, codesign(자체 서명 인증서 `gotto-hando-dev`로 재서명, 9.1), 릴리스
└── .goreleaser.yaml            # darwin/arm64, darwin/amd64, windows/amd64
```

빌드: `CGO_ENABLED=1`은 darwin 타깃만(macOS 호스트에서 빌드). windows 타깃은 `CGO_ENABLED=0 GOOS=windows go build`로 어디서든 크로스 빌드.

---

## 13. 로드맵

| 마일스톤 | 범위 | 완료 기준 |
|---|---|---|
| **M0** 파서 + IR + 로컬 macOS | `syntax`, `ir`, `engine`, `backend/darwin`(입력·캡처·윈도우·클립보드), `output`, `help`(`--help`, `--help-macos`), `--check/--ir`, `--request-perms`, codesign 스크립트 | `gotto-hando local` 로 5장 커맨드 전부 동작(`(M1)`/`(M3)`/`(M4)` 표기 항목 제외). engine 계약 테스트(cua-batch 테스트 이식) 통과. `--help`가 A.1 목차 전부 포함, `--help-macos`가 A.2와 일치 |
| **M1** Windows backend + 프로세스 | `backend/windows`, 세션/DPI/포커스 트릭, 크로스 빌드, `open`·`exec`(5.4; 양 플랫폼, `noerr`, 로그인 셸, 코드페이지 변환), `--help-windows` | Windows에서 `local` 동일 동작. Blender 수치 입력 시나리오 재현. `open`/`exec`가 macOS·Windows `local`에서 동작(출력 캡·타임아웃 kill·CP949 출력의 UTF-8 변환 포함) |
| **M2** `--serve` + 원격 | `transport` 클라이언트/서버, NDJSON, 취소, 프로파일(loopback 검사), 토큰, `--help-remote` | Mac 클라이언트 → SSH 터널 → Windows 서버로 캡처 왕복 < 500ms |
| **M3** 윈도우/앱 편의 | `win[wait=]`, `qwin` 필터, 비-US 레이아웃 키 매핑 점검 | |
| **M4** 캡처 고도화 | ScreenCaptureKit, DXGI, `fmt=jpg`, `cap[cursor]`, 버스트 성능 | |
| **M5** 운영 편의 | 프로파일 `ssh=`로 자동 터널, LaunchAgent/작업 스케줄러 설치 헬퍼 | |

---

## 14. Open questions

확정되어 본문으로 옮긴 항목(더 이상 open이 아님): space-form 폐지·bracket-form 필수(4.1), `exec`/`open` 임의 실행 허용(5.4, 10.1), `rect=` 값의 `:` 구분자(4.2), `scroll` 틱 수 payload 단일화(5.2), `%`+`r` 금지·프레임 모디파이어 단일(4.5), `[f]` 경로 trim(4.2), 코드 8키에 flag 포함(5.1), 조회 커맨드의 명시 `d=`(5.7), `-f`/argv 혼용 금지(7.1), 서버 측 데드라인 강제(6.3), `cap[cursor]`(5.5), Meta=`m`·Win 별칭 flag 없음(4.4), 기본 delay 100ms(6.2), `--help` 계열 영어·`--help [topic]` 미채택·플랫폼/원격 도움말 3종 분리(3장), `exec` exit ≠ 0 기본 `err` + `noerr` flag(5.4, 6.3), `exec`·`open` 기본 delay 제외(5.4), `exec[shell]` = 로그인 셸(`$SHELL -lc`/`/bin/zsh -lc`)·Windows `cmd /C`(5.4), Windows `exec` 출력 코드페이지→UTF-8 변환·U+FFFD 치환(5.4, 9.2), `exec` timeout 상한 60s·IR 필드명·결과 줄 형식(5.4, 5.8, 7.2, 8.1), 프로파일 `host` loopback 아니면 거부(8.4), `--request-perms`(7.1, 9.1).

1. **`p`(primary) 외에 `c`를 primary로 재해석하는 호환 옵션**(`--ctrl-is-primary`)이 필요한가? 현재는 미제공.
2. **base64 인라인 vs blob 엔드포인트**: 버스트 캡처(`n=120`)에서 스트림 크기가 문제면 blob 방식 추가.
3. **`txt`의 `\n` 처리**: Return 키 입력으로 정의했으나, 일부 앱은 Shift+Enter가 개행이다. `txt[nl=key|char]` 옵션 필요 여부.
4. **줄 단위 soft-fail(비-`exec`)**: `-k` 전역 옵션만 제공. `exec`는 `noerr`로 해결됐지만, 특정 줄만 실패 허용(예: 있을 수도 없을 수도 있는 다이얼로그 닫기)하는 일반 문법(`win[opt]...`?)이 필요한지.
5. **키 이름 별칭 유지 범위**: `meta`=`cmd`=`win`은 확정. `option`=`alt`, `return`=`enter` 등 나머지 별칭을 유지할지.
6. **클립보드 복원**: `paste` 후 이전 클립보드를 복원하는 옵션(`restore` flag). 사람과 데스크톱을 공유하는 경우 유용.
7. **Windows 관리자 권한 앱**: UIPI 차단을 서버가 감지해 `E_INPUT` 힌트를 줄 수 있는가(`GetLastError`로는 구분 불가한 경우 많음). 현재는 `qinfo`의 `elevated=` 보고와 `--help-windows` 안내로 대신한다(9.2).
8. **여러 서버 동시 제어**: 한 호출에서 여러 프로파일을 섞는 문법은 제공하지 않음(각 호출 = 한 PC). 확정 여부.

---

## 부록 A. `--help` 계열 본문 초안

아래는 바이너리에 임베드될 네 도움말 텍스트의 초안(영어; 3장에서 확정). 5장 레퍼런스·9장·10장과 동기화 유지가 필요하며, 릴리스 전 실제 동작과 대조한다. 모든 예제는 bracket-form이다. 네 텍스트는 서로 중복하지 않고 상호참조만 한다(3.1).

| 플래그 | 원본 | 대략 분량 |
|---|---|---|
| `--help` | A.1 (5장, 6장, 7장) | ~250줄 |
| `--help-macos` | A.2 (9.1, 10.3 mac 행) | ~120줄 |
| `--help-windows` | A.3 (9.2, 10.3 win 행) | ~90줄 |
| `--help-remote` | A.4 (10.1, 10.2, 8.4) | ~70줄 |

### A.1 `--help`

```
gotto-hando 0.1.0 — drive a macOS/Windows desktop from the shell, one command per line.

== SYNOPSIS ==
  gotto-hando <profile> [options] [line ...]
  gotto-hando <profile> -f <file|->          read lines from a file or stdin. -f and [line ...] cannot be mixed (exit 2).
  gotto-hando <profile>                      stdin piped: read it; stdin is a TTY: run `qinfo`
  gotto-hando --serve <port> [--token <t>] [--detach]   run the server on 127.0.0.1:<port> (see --help-remote; --detach: Windows, detach console)
  gotto-hando local|<profile> --request-perms macOS only: trigger the Accessibility / Screen Recording permission prompts (see --help-macos)
  gotto-hando --help | --help-macos | --help-windows | --help-remote | --profiles | --version

  <profile>   `local` = this machine (in-process). Anything else = an entry in profiles.toml (see PROFILES, --help-remote).
  Options: -f FILE  -k/--keep-going  --delay DUR  --out DIR  --jsonl  --check  --ir  --cap-on-error
           --timeout DUR(300s; sent to the server as the deadline and enforced there too)  --ping  -q/--quiet
  This text is the complete manual for USING the tool. Platform setup (permissions, sessions, signing, DPI) and remote
  setup (server, tunnel, profiles) are separate texts: see SEE ALSO at the end.

== QUICK START ==
  $ gotto-hando local 'win[]Safari' 'k[m]l' 'txt[]example.com\n' 'sleep[]1s' 'cap'
  out /tmp/gotto-hando/local/20260907T131500-a1b2/
  1 ok win id=771 app=Safari matched=1 0,25 1440x875 "Start Page"
  2 ok k
  3 ok txt chars=12
  4 ok sleep
  5 ok cap /tmp/gotto-hando/local/20260907T131500-a1b2/0000-cap-20260907T131501.502Z.png 1440x900 origin=0,0 scale=1
  done ok=5 err=0 skip=0 elapsed=1610ms held_released=0

  Read the PNG, pick coordinates from it (image pixels == input coordinates at scale=1), then act:
  $ gotto-hando local 'c[]312,140' 'txt[ms=20]hello' 'k[]enter' 'cap'
  If the first run exits 4 (E_PERMISSION / E_SESSION), run `gotto-hando local qinfo` and read --help-macos or --help-windows.

== SYNTAX ==
  One line = one command. Blank lines are ignored; lines starting with # are comments.
  Every line is parsed and validated before anything runs. A syntax error anywhere runs nothing (exit 2).

    <command>[<modifiers>]<payload>      payload = everything after the FIRST ']' to end of line. [] is fine (no modifiers).
    <command>                            no payload

  There is NO space-separated form. `m 1024,133`, `k enter` and `m1024,133` are syntax errors: write `m[]1024,133`, `k[]enter`.
  (One rule instead of two, no "exactly one separator" caveat, and the payload always starts right after ']'.)
  <command>    lowercase mnemonic (k, txt, m, c, cap, win, qwin, exec, ...).
  <modifiers>  comma-separated list of single-letter flags and key=value pairs. Flags may be concatenated: [cs] == [c,s].
               Values cannot contain ',' or ']'. A list inside a value is ':'-separated (rect=0:0:600:400).
               Unknown or duplicate modifiers are errors.
  <payload>    raw to end of line. Text commands (txt/paste/clip) keep leading/trailing spaces verbatim and apply escapes;
               all other commands trim it. With [f] the payload is a file path and is always trimmed.
               Payload may contain '[' and ']' freely (only the first ']' after the command closes the modifiers).

  A modifier token containing '=' is a key=value pair; otherwise it is a group of flags ([ms] = flags m+s, [ms=20] = key ms).
  Common modifier on every command:  d=DUR  wait this long after the command. Overrides the global delay, and also applies to
                                     query/sleep/set/cap/exec/open, which get no delay by default.
  Modifier-key flags c s a m p (see MODIFIER KEYS) are accepted by k m c md mu drag scroll only.
  Durations: number = milliseconds; suffix s or ms allowed (500, 1.5s, 66ms).

  Coordinates: x,y in LOGICAL coordinates (macOS points, Windows physical pixels). Origin = top-left of the primary display,
  y grows downward, multi-monitor values may be negative. Frames — at most ONE frame modifier (r, w, disp=) per command:
    (none)  absolute desktop        m[]1024,133
    r       relative to pointer     m[r]10,-5          (N% cannot be combined with r: syntax error)
    w       relative to the current window (set by `win`; otherwise the OS-focused window)   c[w]200,80
    disp=N  relative to display N   m[disp=1]0,0
    N%      percent of the frame    c[w]50%,50%        c[]50%,50% = center of the desktop
  Absolute and disp= coordinates outside the desktop are rejected in preflight (exit 4, nothing runs);
  r/w/% coordinates depend on run-time state and are checked when their line runs (that line fails with E_BOUNDS).
  Display scale factors (Retina, per-monitor DPI) are handled for you at scale=1; details in --help-macos / --help-windows.

  Text escapes (txt/paste/clip only): \n \t \\ \uXXXX. Any other backslash sequence is an error (write C:\\Users).
  In `txt`, \n presses Return and \t presses Tab. In paste/clip they are literal characters.
  [f] flag: payload is a LOCAL file path (client side, trimmed); its UTF-8 contents (<= 64 KiB) are used, no escapes applied.

== MODIFIER KEYS ==
  Single-letter flags, held for the whole command. Pressed in the order c, s, a, m/p; released in reverse.
    flag  meaning   macOS      Windows
    c     Control   Control    Ctrl
    s     Shift     Shift      Shift
    a     Alt       Option     Alt
    m     Meta      Command    Win
    p     Primary   Command    Ctrl      (the platform's copy/paste/save modifier)
  Accepted by: k m c md mu drag scroll.  Not accepted by txt/kd/ku (hold across lines with `kd[]ctrl` ... `ku[]ctrl`).
  There is no separate flag for the Win key: `m` is Meta (Command/Win); `w` is the window FRAME flag. The key name `win`
  (= meta = cmd) is fine inside a payload: k[]win.
  CAVEAT: `c` is ALWAYS Control, even on macOS. Use `p` for the platform's copy/paste/save modifier, `m` for Command/Win explicitly.
          `k[c]c` in a macOS terminal sends ^C (interrupt), NOT Cmd+C. Check `qinfo` (primary=cmd|ctrl) if unsure.

== COMMANDS ==
  KEYBOARD
    k      [n=1 repeat, ms=30 gap]      keys...      press keys. Space = one after another; '+' = chord (press in order, release reversed).
                                                     A chord holds at most 8 keys INCLUDING modifier flags (k[csam]a = 5 keys).
                                                     k[c]v   k[]enter enter   k[n=3]tab   k[]ctrl+shift+a   k[p]s
    kd                                  keys...      hold keys down until `ku` or end of run (auto-released with a warning).
    ku                                  keys...      release keys held by kd. Releasing a key not held is a validation error.
    txt    [ms=0 per-char gap, f]       text|file    type Unicode text. No modifier-key flags allowed.
  MOUSE
    m      [ms=0 duration, r|w|disp=]   x,y          move pointer. ms>0 interpolates the motion.
    c      [b=left|right|middle, n=1 clicks, ms=60 gap, r|w|disp=]  [x,y]   click (moves first if x,y given). c[n=2]  c[b=right]100,200  c[s]
    md     [b=, r|w|disp=]              [x,y]        press and hold a button.
    mu     [b=]                                      release a button held by md.
    drag   [b=left, ms=500 total, steps=20, r|w|disp=]  x1,y1 x2,y2 [x3,y3 ...]   move to first point, press, follow the polyline, release.
                                                     One point = drag from the current position.
    scroll [by=line|page]               up|down|left|right [ticks]   scroll at the current pointer position; ticks default 3 (1..50).
                                                     The tick count is payload only (no n=). Move first with `m`.  scroll[]down 2  scroll[by=page]down
  CLIPBOARD
    clip   [f]                          text|file    set the target clipboard (no paste).
    paste  [f, ms=50 settle]            text|file    set clipboard, wait ms, press primary+v. Never presses Return. Clipboard is not restored.
    qclip  [f]                          [file]       print clipboard text (\n-escaped), or save it to a local file with [f].
  WINDOWS / APPS / PROCESSES
    win    [r regex, wait=0]            selector     find, unminimize, raise and focus a window; it becomes the current window for [w].
    qwin   [r]                          [selector]   list visible windows (focused one marked *).
    selector: id:<n> | pid:<n> | app:<name> | <title substring, case-insensitive>. Multiple matches: frontmost wins, matched=N is reported.
    open   [wait=0]                     app|path     launch or activate an app or run a path (macOS `open -a`/`open <path>`, Windows ShellExecute).
                                                     No default delay (like exec): use open[wait=5s], win[wait=], sleep or d= to wait for the UI.
    exec   [timeout=10s, shell, noerr]  command...   run a command ON THE TARGET machine and wait for it (timeout <= 60s).
                                                     Without `shell`: split the payload on spaces ("..." quoting only, no escapes, no
                                                     expansion) and run it directly. With `shell`: hand the whole payload to the LOGIN shell
                                                     `$SHELL -lc` (macOS; /bin/zsh -lc if $SHELL is unset) or `cmd /C` (Windows), so PATH from
                                                     your shell rc files is available even when the server was started by launchd / Task
                                                     Scheduler. cwd and environment = the server process (local: this process); stdin is closed.
                                                     Reports exit code, stdout and stderr (each <= 64 KiB, UTF-8 — Windows output is converted
                                                     from the console code page; truncated=1 if cut). Non-zero exit = err E_EXEC and stops the
                                                     run, UNLESS `noerr`: then the line is ok and exit=N is just reported (for grep/diff-style
                                                     commands). Timeout kills the process = err E_TIMEOUT (never softened by noerr). -k continues
                                                     past either. exec never gets the default delay (d= works).
                                                     exec[]git -C "C:\work dir" status    exec[shell]dir /b *.blend    exec[timeout=60s]blender -b x.blend -f 1
                                                     exec[noerr]grep -q TODO notes.txt   -> ok exec exit=1 ...
  CAPTURE
    cap    [w|disp=N, rect=x:y:w:h, scale=1|native, fmt=png|jpg, q=85, n=1 frames, ms=100 gap, label=cap, cursor]  [path]
           save a screenshot to a LOCAL file and print its path, size, origin and scale. Default scale=1 makes image pixels
           equal input coordinates (Retina is downscaled; use scale=native for full pixels). n>1 writes -00, -01, ...
           rect = x:y:w:h (':'-separated) in logical coordinates of the chosen frame. cursor draws the pointer into the image (off by default).
  FLOW
    sleep                               DUR          wait (<= 60s).
    set    [delay=, txtms=, keyms=]                  change defaults for the following lines (delay: pause after every command except
                                                     query/sleep/set/cap/exec/open, default 100ms; an explicit d= always applies).
  QUERY (no default delay; an explicit d= still applies)
    qinfo                                            os, version, primary modifier, desktop bounds, displays, session, permissions.
                                                     perms=accessibility:ok|missing,screen:ok|missing (macOS) | perms=n/a elevated=0|1 (Windows)
                                                     session=active|locked|inactive. Anything but ok/active: see --help-macos / --help-windows.
    qdisp                                            one line per display: idx, x,y WxH, scale, primary.
    qmouse                                           current pointer position.

== KEY NAMES ==
  a-z 0-9 f1-f24 ctrl shift alt|option meta|cmd|win primary enter|return tab esc|escape space backspace delete insert
  home end pageup pagedown up down left right period comma minus equal slash backslash semicolon quote grave lbracket rbracket
  numpad0-numpad9 decimal numadd numsub nummul numdiv numenter capslock printscreen scrolllock pause volup voldown mute
  Names are PHYSICAL keys (US layout positions), not characters. To enter characters, use txt.

== STATE MACHINE ==
  Within one invocation, lines share state: the current window (`win`), keys/buttons held by kd/md, the pointer position,
  and defaults from `set`. Nothing persists between invocations; the server keeps no state either.
  Any held key/button is released in reverse order when the run ends, fails, times out, or is interrupted (Ctrl-C, connection loss).

== OUTPUT ==
  First line:  out <capture directory>
  Per line:    <line-number> <ok|err|skip|warn> <command> <details...>
               Multi-line results (qwin, qdisp, exec output) are indented by two spaces, fields TAB-separated.
               exec:  <n> ok exec exit=0 ms=41 stdout=34B stderr=0B   followed by one line per output line:  "  1<TAB>text" (1=stdout, 2=stderr).
                      The output lines follow err results too (non-zero exit, timeout). With [noerr] a non-zero exit is still `ok`;
                      read exit=N yourself.
               Errors: <line> err <source>: <message> (<E_CODE>)
  Last line:   done ok=N err=N skip=N elapsed=MSms held_released=N [state=unknown]   (state=unknown only with exit 5 = connection lost)
  --jsonl:     one JSON object per event ({"event":"start"...}, per-line objects, {"event":"done"...}); exec objects carry exit, stdout, stderr, truncated.
  Line numbers count every input line including comments and blanks, so they match your input (argv or -f, never both).
  `ok` means the OS accepted the input; it does NOT prove the application reacted. Verify with `cap`.
  --timeout expiry is NOT exit 5: the server enforces the same deadline, kills the running line (err E_TIMEOUT), releases held keys,
  skips the rest and prints a normal `done` (exit 1).

== LIMITS ==
  1000 lines; 64 KiB per line and per [f] file; k: 64 sequential keys, 8 per chord including modifier flags; scroll 1..50 ticks;
  sleep <= 60s; d=/ms= <= 10s; wait= <= 60s; exec timeout= <= 60s (default 10s), exec stdout/stderr <= 64 KiB each;
  drag 1..200 payload points (path 2..200 including the current position), steps 1..200; label [A-Za-z0-9][A-Za-z0-9_-]{0,47};
  cap n <= 120 and <= 121 captures per run; --timeout default 300s (enforced by client and server).

== CAVEATS ==
  CAVEAT: Unicode text injection (txt) is ignored by some apps (Blender, many games, Win32 consoles). Use `k` per key
          (digits, period, minus, numpad*) or `paste` instead, then verify with `cap`.
  CAVEAT: paste never presses Return, but a console/editor may execute pasted newlines. For a Python console, paste a single-line
          exec(compile(...)) without a trailing newline, inspect with cap, then `k[]enter`.
  CAVEAT: Ctrl+V is not "paste" in every app (terminals). Use `clip[]...` followed by `k[s]insert` or `k[cs]v`.
  CAVEAT: Every line with a payload contains [ and ]. Always single-quote lines (zsh aborts with "no matches found" otherwise,
          bash may glob-expand them). `txt[]  x` types two spaces then x; other commands trim the payload.
  CAVEAT: Backslash sequences other than \n \t \\ \uXXXX are errors in txt/paste/clip. Escape Windows paths (C:\\Users).
          In every other command (exec, cap, ...) a backslash is an ordinary character.
  CAVEAT: An active IME (e.g. Korean) composes `k` letters. Prefer txt for text; check with cap.
  CAVEAT: `win` reports E_NOWINDOW when the OS refused to focus the window (Windows foreground rules) and E_INPUT when the
          platform blocks synthetic input (macOS Secure Input, Windows elevated apps). Platform details: --help-macos, --help-windows.
  CAVEAT: exec runs with the server account's privileges — the same power as that account's shell (see --help-remote). There is
          no allowlist. exec[shell] uses the login shell, so PATH is usually complete; if a command is still not found, use an
          absolute path.
  CAVEAT: Frame bursts (cap[n=]) are best effort; each result reports the actual capture time and slippage in --jsonl.
  CAVEAT: If the run ends with exit 5 (connection lost), held keys may still be down on the target. Run `qinfo` and `cap` before
          sending more input.
  CAVEAT: exit 4 before anything ran means the TARGET is not ready (permissions, locked/inactive session, physical key held,
          absolute coordinate out of bounds). Read the message; for perms/session fixes see --help-macos / --help-windows.

== SHELL QUOTING ==
  [ ] ! * ? $ ` and spaces are special to shells, and every payload line contains [ ]. Single-quote every line, or use a
  quoted heredoc / -f:
    gotto-hando local 'k[c]a' 'txt[]hello, world!' 'cap'
    gotto-hando winbox <<'EOF'
    win[]Blender
    k[c]a
    cap
    EOF
  Do not double-quote lines containing ! in zsh/bash (history expansion). A newline inside one argument splits it into lines.
  PowerShell/cmd.exe: prefer -f FILE or stdin. Large paste payloads must use [f] (argv limits).

== PROFILES ==
  <profile> names a target PC. `local` is built in and cannot be redefined. Others are read from
  ~/.config/gotto-hando/profiles.toml (macOS/Linux; $XDG_CONFIG_HOME honored) or %APPDATA%\gotto-hando\profiles.toml (Windows).
  `gotto-hando --profiles` prints the file path and the entries. Unknown profile = exit 3.
  The file format and how to set up a remote PC are in --help-remote.

== EXAMPLES ==
  # Focus Blender, select all, numeric transform via individual keys (Unicode text is ignored by Blender)
  gotto-hando winbox 'win[]Blender' 'k[]a' 'k[]g' 'k[]x' 'k[]1 period 5' 'k[]enter' 'cap'
  # Paste a script into a console window without executing, inspect, then run
  gotto-hando winbox 'win[r]Python Console' 'paste[f]./cmd.py' 'cap[w]' && gotto-hando winbox 'k[]enter' 'sleep[]500' 'cap[w]'
  # Middle-button orbit and a burst of frames
  gotto-hando winbox 'm[]960,540' 'drag[b=middle,ms=300]960,540 1100,540' 'cap[n=3,ms=100,label=orbit]'
  # Shift-click two items, right-click, screenshot the window only
  gotto-hando local 'win[]Finder' 'c[w]120,200' 'c[ws]120,240' 'c[w,b=right]120,240' 'cap[w,scale=0.5,fmt=jpg]'
  # Relative nudge, hover, region capture
  gotto-hando local 'm[r]0,-40' 'sleep[]300' 'cap[rect=0:0:600:400]'
  # Scroll two pages down at a position
  gotto-hando local 'm[]700,500' 'scroll[by=page]down 2'
  # Hold a modifier across several actions
  gotto-hando local 'kd[]shift' 'c[]100,100' 'c[]300,100' 'ku[]shift'
  # Save the target clipboard to a local file
  gotto-hando winbox 'k[c]a' 'k[c]c' 'qclip[f]./selection.txt'
  # Launch an app, wait for its window, then run a command on the target and read its output
  gotto-hando winbox 'open[wait=10s]blender' 'win[]Blender' 'exec[]blender --version' 'exec[shell]dir /b C:\work\*.blend'
  # Render headless on the target and capture the result folder listing (long-running: raise the exec timeout)
  gotto-hando winbox 'exec[timeout=60s]blender -b C:\work\scene.blend -f 1' 'exec[shell]dir /b C:\work\render'
  # Probe without failing the run: grep exits 1 when nothing matches
  gotto-hando local 'exec[noerr,shell]grep -q ERROR ~/app.log' 'cap'
  # Machine-readable
  gotto-hando winbox --jsonl 'qwin' 'qdisp'

== EXIT CODES ==
  0 all ok   1 some command failed at runtime (includes --timeout expiry: E_TIMEOUT, held keys released)
  2 syntax/validation error (nothing ran)   3 profile/connection error (nothing ran)
  4 preflight failed: permission/session/absolute-coordinate bounds/key support (nothing ran)
  5 state unknown (connection lost mid-run; held keys may still be down)

== SEE ALSO ==
  --help-macos     when `qinfo` shows perms=...:missing or session!=active on a Mac, after rebuilding on macOS, or before
                   running --serve on a Mac. Signing identity, TCC permissions, --request-perms, LaunchAgent, Retina.
  --help-windows   when `qinfo` shows session!=active on Windows, input is silently ignored by an elevated app, or before
                   running --serve on Windows. Interactive session, Task Scheduler, SmartScreen, UAC/UIPI, DPI, cmd /C.
  --help-remote    to control a PC other than `local`, or on exit 3 (E_CONNECT). Server --serve/--token, ssh -L tunnel,
                   profiles.toml, verification, the security model.
```

### A.2 `--help-macos`

```
gotto-hando --help-macos — macOS: signing, permissions (TCC), sessions, displays.

== WHEN YOU NEED THIS ==
  `gotto-hando local qinfo` shows perms=accessibility:missing or screen:missing, or session=locked|inactive;
  a run exited 4 with E_PERMISSION / E_SESSION; you rebuilt the binary and input stopped working;
  you are about to run `gotto-hando --serve` on a Mac (especially one you reach only over SSH).

== CHECK ==
  $ gotto-hando local qinfo
  os=darwin osver=14.5 arch=arm64 ver=0.1.0 primary=cmd desktop=0,0 2560x1440 displays=2 session=active perms=accessibility:ok,screen:ok
  perms=accessibility:*   gates ALL input (k txt m c drag scroll ...) and window control (win). Read with AXIsProcessTrusted().
  perms=screen:*          gates cap and window titles in qwin (without it titles come back empty). Read with CGPreflightScreenCaptureAccess().
  session=active          GUI session, unlocked.  locked = lock screen / screen saver.  inactive = not a GUI session (SSH shell).
  qinfo never prompts. Preflight fails with exit 4 when anything above is not ok/active.

== LOCAL BUILD AND RUN ==
  `go build` / `go install` / `git clone` + build: the Go linker ad-hoc signs the binary; that is all a local binary needs.
  Such binaries carry no quarantine attribute and start normally.
  A binary DOWNLOADED with a browser is quarantined and blocked ("cannot be opened", "not verified"). Remove the attribute:
    xattr -d com.apple.quarantine ./gotto-hando
  Notarization is NOT needed for this tool (it requires a paid Apple Developer Program membership). Do not chase it.

== WHO OWNS THE PERMISSION (RESPONSIBLE PROCESS) ==
  TCC attributes a permission to the "responsible" app of the process. When gotto-hando runs from a terminal application
  (Terminal, iTerm2, the VS Code integrated terminal, ...) that app is responsible: the toggle you see in System Settings is
  for Terminal/iTerm2/Code, not for gotto-hando. Consequences:
    - Local use (`gotto-hando local ...` typed into a terminal): grant the permission ONCE to the terminal app. Rebuilding
      gotto-hando does not affect it. Nothing else to do.
    - `--serve` started by launchd (LaunchAgent) or from an SSH shell: the binary itself is responsible, so the permission is
      bound to the binary's code-signing identity (next section) and must be granted on that Mac by hand (REMOTE MAC below).
  Also note: an agent running INSIDE a terminal-hosted tool inherits that terminal's grant.

== STABLE SIGNING IDENTITY (SURVIVING REBUILDS) ==
  TCC binds a grant to the code-signing identity. An ad-hoc signature has no identity — its designated requirement is the
  hash of that exact build — so every rebuild is a new "app" and the grant is lost (even with a fixed --identifier).
  Fix: sign with a self-signed code-signing certificate, which gives the binary a stable identity across rebuilds.
    1. Keychain Access > Certificate Assistant > Create a Certificate...
       Name: gotto-hando-dev   Identity Type: Self Signed Root   Certificate Type: Code Signing   -> Create.
    2. After every build:
       codesign -s "gotto-hando-dev" --force ./gotto-hando
       codesign -dv ./gotto-hando     # shows Authority=gotto-hando-dev
    3. Put step 2 in your Makefile / build script (the repo's scripts/ does this). Grant the permission once per Mac for
       this identity; later rebuilds keep it.
  This matters only where the binary itself is the responsible process (--serve via launchd / SSH). For terminal use, the
  terminal app's grant covers you and this step is optional.
  A binary re-signed with a different certificate (or ad-hoc again) is a new identity: grant again.

== GRANTING PERMISSIONS: PROCEDURE ==
  Do this in order; stop as soon as qinfo shows ok. Only a human can flip the toggles — steps 3-4 cannot be automated.
    1. gotto-hando local qinfo                      # which of accessibility / screen is missing?
    2. gotto-hando local --request-perms            # asks macOS to show the permission prompts
       (remote Mac: gotto-hando <profile> --request-perms — the SERVER shows them in its own GUI session; see REMOTE MAC)
       - Calls AXIsProcessTrustedWithOptions(prompt) and CGRequestScreenCaptureAccess(). Prints the same perms= line and
         exits 0 when both are ok, 4 otherwise.
       - The prompt ADDS the responsible app to the list in System Settings but does not turn it on; a person must toggle it.
       - Must run inside the GUI session (a terminal window, or the LaunchAgent). From an SSH shell there is NO prompt:
         it just prints missing. Then go to step 3 on the machine itself.
       - Screen Recording takes effect only after the process is RESTARTED (quit the terminal tab or restart --serve).
    3. Tell the user exactly this:
         System Settings > Privacy & Security > Accessibility     -> enable <Terminal app | gotto-hando>
         System Settings > Privacy & Security > Screen Recording  -> enable <Terminal app | gotto-hando>
       If the entry is absent, press "+" and choose the binary (for --serve) or the terminal app. Then restart gotto-hando.
       (macOS 15+ may re-confirm Screen Recording periodically; the same toggle.)
    4. Re-run step 1. If accessibility is still missing after toggling, the identity changed (rebuild without a stable
       signature): remove the stale entry with "-" and add the binary again, or `tccutil reset Accessibility` and redo.
  `tccutil reset Accessibility|ScreenCapture [<bundle-id>]` only CLEARS grants; it cannot grant.
  MDM (PPPC profile) can pre-approve Accessibility but cannot pre-approve Screen Recording — that one is always a manual toggle.

== REMOTE MAC (RUNNING --serve) ==
  SSH login shells are not part of the Aqua (GUI) session: events posted from them are dropped and captures are blank,
  and permission prompts never appear (session=inactive). Start the server INSIDE the GUI session:
    - Interactive: from a Terminal window on that Mac (or via Screen Sharing), or
    - Unattended: a LaunchAgent (runs in the user's GUI session, restarts on login):
        ~/Library/LaunchAgents/io.gotto-hando.serve.plist  (chmod 600 — it holds the token)
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0"><dict>
          <key>Label</key><string>io.gotto-hando.serve</string>
          <key>ProgramArguments</key><array>
            <string>/usr/local/bin/gotto-hando</string><string>--serve</string><string>47311</string>
            <string>--token</string><string>s3cr3t</string></array>
          <key>RunAtLoad</key><true/>
          <key>KeepAlive</key><true/>
          <key>LimitLoadToSessionType</key><string>Aqua</string>
          <key>StandardOutPath</key><string>/tmp/gotto-hando.log</string>
          <key>StandardErrorPath</key><string>/tmp/gotto-hando.log</string>
        </dict></plist>
        launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.gotto-hando.serve.plist
        launchctl kickstart -k gui/$(id -u)/io.gotto-hando.serve      # restart after a permission change or rebuild
  Permissions for a launchd-started server belong to the BINARY (stable identity required, see above) and must be granted
  once per Mac per identity, by a person at the machine or over Screen Sharing: System Settings > Privacy & Security >
  Accessibility and Screen Recording > "+" > select the binary. Then restart the agent (kickstart). Verify over the tunnel:
    gotto-hando <profile> qinfo     -> session=active perms=accessibility:ok,screen:ok
  Shortcut over the tunnel — pop the prompts on the remote Mac without Screen Sharing:
    gotto-hando <profile> --request-perms
  The client calls the server's POST /v1/request-perms and the --serve process itself calls AXIsProcessTrustedWithOptions /
  CGRequestScreenCaptureAccess, so the system prompts appear in the GUI session the SERVER lives in. This only works when
  the server runs inside the Aqua session (the LaunchAgent above); a server started from an SSH shell shows nothing and
  just returns missing. The prompt adds the binary to the lists but a person at that Mac still has to flip the toggles,
  and Screen Recording is picked up only after the server restarts (launchctl kickstart -k ...). Then verify with qinfo.
  Against a Windows server the command is a no-op (perms=n/a, exit 0).
  Keep the Mac awake and unlocked while it is being driven: `caffeinate -d -i` in another LaunchAgent, or System Settings >
  Lock Screen > never require password / never turn display off. A locked or sleeping Mac reports session=locked (exit 4).
  Tunnel, profile and --token: see --help-remote.

== SESSION, LOCK, SECURE INPUT ==
  session=locked   lock screen or screen saver is up. Input and capture fail in preflight (exit 4). Unlock; prevent with caffeinate.
  session=inactive process is not in the GUI session (SSH). Start it from Terminal / LaunchAgent (above).
  Secure Input      while a password field (or an app that enables Secure Event Input) is focused, synthetic key events are
                    blocked. The failing line reports E_INPUT with a hint. Click elsewhere / close the dialog and retry.
  Fast user switching: only the session that owns the console can be driven.

== DISPLAYS AND COORDINATES ==
  Coordinates are in points (logical). On Retina displays one point = 2 pixels (scale=2 in `qdisp`); a default `cap` is
  downscaled so image pixels == points, and you can feed pixel positions from the PNG straight back as coordinates.
  cap[scale=native] keeps full pixels — divide by the display's scale before using them as input coordinates.
  Multi-display: origin (0,0) is the top-left of the primary display; displays to the left/above have negative coordinates.
  Displays can have different scales; `qdisp` lists idx, origin, size and scale, and m[disp=N] / cap[disp=N] address one display.
  `qwin` frames include the title bar; c[w]x,y is relative to the window's top-left INCLUDING the title bar.

== CAVEATS ==
  CAVEAT: Rebuilt and input stopped? qinfo -> perms missing -> the identity changed. Use the self-signed certificate step
          or, for terminal use, nothing at all (the terminal app owns the grant).
  CAVEAT: `open[]Name` uses `open -a Name` (bundle name, case-insensitive); a path uses `open <path>`. A wrong name is E_EXEC.
  CAVEAT: exec[shell] runs `$SHELL -lc` (zsh by default): your ~/.zprofile / ~/.zshrc PATH applies, even under launchd.
  CAVEAT: k[]a etc. are physical US-layout keys; with a non-US or IME input source use txt for text.
  CAVEAT: Some apps only honour modifier flags carried on the event, not separate modifier key-downs; gotto-hando sets both.
          If a chord is ignored, try k[]ctrl+shift+a form vs k[cs]a and verify with cap.
```

### A.3 `--help-windows`

```
gotto-hando --help-windows — Windows: interactive session, Task Scheduler, SmartScreen, UAC/UIPI, DPI, cmd /C.

== WHEN YOU NEED THIS ==
  `gotto-hando local qinfo` shows session=locked|inactive; a run exited 4 with E_SESSION; input is accepted (ok) but an
  elevated app does not react; you are about to run `gotto-hando --serve` on a Windows PC; exec output looks mojibake.

== CHECK ==
  $ gotto-hando local qinfo
  os=windows osver=10.0.22631 arch=amd64 ver=0.1.0 primary=ctrl desktop=0,0 2560x1440 displays=1 session=active perms=n/a elevated=0
  session=active    this process is in the active console session and the input desktop is "Default": input and capture work.
  session=locked    the input desktop is Winlogon: lock screen, UAC secure desktop, Ctrl+Alt+Del screen. Unlock / dismiss.
  session=inactive  not the active console session: service (session 0), a disconnected RDP session, another user's session.
  perms=n/a         Windows has no permission dialogs for input/capture. Nothing to grant.
  elevated=0|1      whether gotto-hando runs as Administrator (matters for UIPI, below).

== INTERACTIVE SESSION REQUIRED ==
  SendInput and screen capture only work from a process inside the logged-on user's interactive desktop.
    - NOT a Windows service / session 0 (always E_SESSION). Do not wrap --serve in a service (sc.exe, NSSM, srvany).
    - NOT a locked desktop: the lock screen, screen saver with password, or a UAC prompt switch the input desktop to Winlogon
      (session=locked). Disable automatic lock while driving: Settings > Accounts > Sign-in options > "Require sign-in: Never",
      and Power & sleep / screen timeout: Never. Autologon (netplwiz / Sysinternals Autologon) restores the session after reboot.
    - RDP: while you are connected over RDP the RDP session is the active one; DISCONNECTING locks it (session=inactive/locked).
      To hand the desktop back to the physical console without locking it, run from inside the RDP session (elevated):
        tscon %SESSIONNAME% /dest:console
      Then keep the console unlocked (settings above). A headless PC without a monitor may need a dummy HDMI/DP plug so a
      display exists to capture.
    - SSH (OpenSSH Server) shells are session-0-like: `gotto-hando --serve` started from an SSH shell reports session=inactive.
      Use Task Scheduler (below) so the server lives in the interactive session; use SSH only for the tunnel.

== KEEPING --serve RUNNING (TASK SCHEDULER) ==
  Run the server as a normal process of the logged-on user, started at logon:
    GUI: Task Scheduler > Create Task
      General:   "Run only when user is logged on"  (REQUIRED — "whether user is logged on or not" has no interactive desktop)
                 "Run with highest privileges" only if you need to drive elevated apps (UIPI, below)
      Triggers:  At log on — specific user
      Actions:   Start a program: C:\tools\gotto-hando.exe   Arguments: --serve 47311 --token s3cr3t --detach
      Conditions: uncheck "Start the task only if the computer is on AC power"
      Settings:  uncheck "Stop the task if it runs longer than"; "If the task is already running: Do not start a new instance"
    Command line equivalent (creates an interactive-only task for the current user):
      schtasks /Create /TN gotto-hando /SC ONLOGON /RL LIMITED /F ^
        /TR "\"C:\tools\gotto-hando.exe\" --serve 47311 --token s3cr3t --detach"
      schtasks /Run /TN gotto-hando           # start now without logging off
      (use /RL HIGHEST from an elevated prompt for an elevated server)
  --detach frees the console (FreeConsole) and writes the log to %LOCALAPPDATA%\gotto-hando\serve.log, so no window stays open.
  A startup-folder shortcut (shell:startup) also works but cannot be elevated without a UAC prompt at every logon.

== SMARTSCREEN AND DEFENDER ==
  A binary downloaded with a browser carries the Mark-of-the-Web; on first start SmartScreen shows "Windows protected your PC".
  A person must click "More info" > "Run anyway" (cannot be automated), or remove the mark beforehand:
    powershell -NoProfile -Command "Unblock-File C:\tools\gotto-hando.exe"
  A binary built locally with `go build` has no mark. Defender occasionally flags unsigned Go binaries as a false positive
  (Trojan:Win32/Wacatac etc.): add the folder to Windows Security > Virus & threat protection > Exclusions, or use a locally
  built binary. There is no code-signing requirement for input/capture.

== UAC AND ELEVATED APPS (UIPI) ==
  While a UAC elevation prompt is showing, Windows switches to the secure desktop: session=locked, nothing can be driven and
  captures fail. gotto-hando cannot click "Yes" for you; a person dismisses it (or the app is started elevated beforehand).
  Apps running as Administrator ignore SendInput from a non-elevated process (UIPI). The OS reports success, so gotto-hando
  prints `ok` and the app does nothing. Check `qinfo` elevated=; to drive elevated apps run the server elevated
  (Task Scheduler "Run with highest privileges" / `schtasks /RL HIGHEST`). An elevated server can drive both kinds.

== DPI AND COORDINATES ==
  Coordinates are physical pixels. gotto-hando declares Per-Monitor-V2 DPI awareness (manifest + SetProcessDpiAwarenessContext),
  so it sees real pixels on every monitor regardless of the scaling percentage, and a default `cap` (scale=1) is pixel-exact:
  a position found in the PNG is the coordinate to click. `qdisp` lists each monitor's origin, size and scale (1.25 = 125%).
  Virtual-desktop origin is the top-left of the PRIMARY monitor; monitors to the left/above have negative coordinates.
  Window frames from `qwin` exclude the invisible drop-shadow border (DWM extended frame bounds).
  An app that is NOT DPI-aware is bitmap-stretched by Windows; its own notion of coordinates differs, but what you see in
  `cap` is what SendInput hits, so always take coordinates from a capture.

== exec ON WINDOWS ==
  exec[]prog args...   direct: the payload is split on spaces ("..." quoting only) and run as a process.
  exec[shell]...       `cmd /C <payload>`: pipes, redirection, built-ins (dir, copy, set), wildcards via the command.
                       For PowerShell call it explicitly:  exec[]powershell -NoProfile -Command Get-Process | Select -First 3
  Output encoding: cmd and many console programs write the OEM code page (CP437, CP949, ...). gotto-hando decodes stdout/stderr
  with the console output code page (GetConsoleOutputCP; the ANSI code page when the server has no console, e.g. --detach) and
  delivers UTF-8. Undecodable bytes become U+FFFD. If a program prints UTF-8 while the console is CP949, switch the code page
  in the same command:  exec[shell]chcp 65001 >nul && type file.txt
  PATH: a Task Scheduler-started server has the user's normal PATH (unlike macOS launchd). Absolute paths are still safest.
  Paths with spaces: exec[]"C:\Program Files\Blender\blender.exe" --version   (no backslash escaping in exec).

== CAVEATS ==
  CAVEAT: SetForegroundWindow may only flash the taskbar button. `win` retries with input-thread attachment and a synthetic
          Alt tap and reports E_NOWINDOW if the window still did not come to the front. Retry once; then click it (c[]x,y).
  CAVEAT: Unicode text injection (txt) is ignored by scan-code apps (Blender, games, Win32 consoles). Use k per key or paste.
  CAVEAT: Keypad keys are sent by scan code (physical position) regardless of NumLock; use numpad0-9 / numenter deliberately.
  CAVEAT: Win32 console windows do not accept Ctrl+V paste by default: use clip[] + k[s]insert or right-click, or exec instead.
  CAVEAT: Store/UWP apps and some games running in exclusive fullscreen may not honour synthetic input; verify with cap.
```

### A.4 `--help-remote`

```
gotto-hando --help-remote — control another PC: server, SSH tunnel, profile.

== WHEN YOU NEED THIS ==
  You want to drive a PC other than `local`; a run exited 3 (E_CONNECT / unknown profile); `--profiles` shows nothing.

== SECURITY MODEL (READ FIRST) ==
  The server (`gotto-hando --serve`) binds 127.0.0.1 ONLY. There is no --bind option and the client refuses profiles whose
  host is not loopback (exit 3). The only supported path to a remote server is an SSH tunnel.
  The server = that account's shell. exec and open run arbitrary commands as the logged-in user, so whoever can reach the
  port can do anything that user can. This adds no power beyond the SSH account you already need for the tunnel — but it
  means: never forward the port beyond localhost, and on a host with other local users always start the server with
  --token and put the same token in the profile. There is no command allowlist.
  The server keeps no state and writes nothing to disk except its log; captures and exec output travel back over the
  tunnel and are stored on the CLIENT.

== SERVER SIDE (the PC to control) ==
  1. Install: copy gotto-hando to the PATH; `gotto-hando --version`.
  2. Prepare the platform (permissions, session, autostart):
       macOS   -> gotto-hando --help-macos     (stable signing identity, Accessibility + Screen Recording, LaunchAgent)
       Windows -> gotto-hando --help-windows   (interactive logon session, Task Scheduler, UIPI)
     Check on the machine:  gotto-hando local --ping   -> session=active and perms all ok / n/a.
  3. Start the server INSIDE the GUI login session (Terminal window, LaunchAgent, or Task Scheduler; NOT an SSH shell, NOT a service):
       gotto-hando --serve 47311 --token s3cr3t            # pick any free port; token = any secret string
     One server per port; it accepts one run at a time (a second client gets HTTP 409 busy).
  4. Enable SSH on the server PC: macOS System Settings > General > Sharing > Remote Login; Windows Settings > Apps >
     Optional features > OpenSSH Server (service "sshd", automatic). Prefer key-based auth.
  5. Verify locally on the server PC:
       curl -s -H 'Authorization: Bearer s3cr3t' http://127.0.0.1:47311/v1/info

== CLIENT SIDE (where the agent runs) ==
  1. Open the tunnel (keep it running; autossh or a systemd/launchd user job is fine):
       ssh -N -f -o ServerAliveInterval=30 -L 47311:127.0.0.1:47311 user@remote-host
     Use a different LOCAL port if 47311 is taken:  -L 47399:127.0.0.1:47311  and put 47399 in the profile.
  2. Register a profile in ~/.config/gotto-hando/profiles.toml (macOS/Linux; $XDG_CONFIG_HOME honored) or
     %APPDATA%\gotto-hando\profiles.toml (Windows). `local` is reserved and cannot be defined here.
       [profiles.winbox]
       host  = "127.0.0.1"      # must be loopback (127.0.0.1, ::1, localhost) — anything else is refused, exit 3
       port  = 47311            # the LOCAL end of the tunnel
       token = "s3cr3t"         # optional; must match --token. Required on shared hosts
       delay = "150ms"          # optional default delay for this PC (overrides the 100ms default; --delay overrides this)
       out   = "~/shots/winbox" # optional default capture directory (--out and $GOTTO_HANDO_OUT override)
     Profile names: [a-z0-9][a-z0-9_-]*.  `gotto-hando --profiles` prints the file path and entries.
  3. Verify:  gotto-hando winbox --ping      -> os=windows ... session=active perms=n/a
              gotto-hando winbox cap         -> a PNG on THIS machine; inspect it, pick coordinates.
  4. Drive it exactly like local:  gotto-hando winbox 'win[]Blender' 'k[]a' 'cap'

== HOW A RUN TRAVELS ==
  The client parses and validates everything (exit 2 on error, nothing sent), then POSTs the IR with your --timeout as the
  deadline. The server streams one result per line as it happens; captures come back inline and are written under --out
  (or $GOTTO_HANDO_OUT, the profile's out, or $TMPDIR/gotto-hando/<profile>/<run-id>/). Text and files given with [f] are
  read on the CLIENT and sent as data; the server never touches your files. Ctrl-C on the client cancels the run on the
  server and releases held keys. If the connection drops mid-run the client exits 5 (state=unknown): run `qinfo` and `cap`
  before sending more input.

== TROUBLESHOOTING ==
  exit 3  unknown profile      -> `gotto-hando --profiles`; check the file path and [profiles.<name>] spelling.
  exit 3  connection refused   -> tunnel not up (`ssh -N -f -L ...`), wrong local port, or the server is not running.
  exit 3  401 unauthorized     -> token mismatch between --token and the profile.
  exit 3  host not loopback    -> put host = "127.0.0.1" and tunnel with ssh -L; direct LAN/VPN access is not supported.
  exit 3  version mismatch     -> client and server must be the same gotto-hando version (IR schema); update both.
  exit 4  E_SESSION            -> the server is not in an active, unlocked GUI session: --help-macos / --help-windows.
  exit 4  E_PERMISSION (macOS) -> Accessibility / Screen Recording not granted to the server binary. Try
                                  `gotto-hando <profile> --request-perms`: the server pops the system prompts in its own
                                  GUI session (LaunchAgent, not an SSH shell — otherwise it just returns missing with no
                                  prompt). A person at that Mac must still flip the toggles; restart the server afterwards
                                  for Screen Recording, then `qinfo`. Details: --help-macos. Windows server: no-op, perms=n/a.
  409 busy                     -> another run is in progress on that server; wait or cancel it (Ctrl-C on that client).
  Blank captures / ignored input with everything "ok" -> server started from an SSH shell or a service; restart it inside
                                  the GUI session (see the platform help).
  Slow first round trip        -> normal (~100 ms over a LAN tunnel). If every run is slow, check ServerAliveInterval and
                                  that autossh is not reconnecting in a loop.
```

---

## 부록 B. IR JSON 예시

입력:

```
win[]Blender
k[c]v
txt[ms=66]hello, world!
m[]1024,133
c[n=2,w]50%,50%
cap[n=2,ms=100,label=after]
```

`--ir` 출력(요약):

```json
{
  "v": 1,
  "ops": [
    {"line": 1, "src": "win[]Blender", "op": "focus", "selector": {"kind": "title", "value": "Blender", "regex": false}, "wait_ms": 0},
    {"line": 2, "src": "k[c]v", "op": "key", "mods": ["ctrl"], "keys": [["v"]], "repeat": 1, "gap_ms": 30},
    {"line": 3, "src": "txt[ms=66]hello, world!", "op": "text", "text": "hello, world!", "interval_ms": 66},
    {"line": 4, "src": "m[]1024,133", "op": "move", "point": {"frame": "desktop", "x": 1024, "y": 133}, "duration_ms": 0},
    {"line": 5, "src": "c[n=2,w]50%,50%", "op": "click", "button": "left", "count": 2, "gap_ms": 60,
     "point": {"frame": "window", "x": 50, "y": 50, "x_pct": true, "y_pct": true}, "mods": []},
    {"line": 6, "src": "cap[n=2,ms=100,label=after]", "op": "capture", "frame": "desktop", "scale": 1.0, "format": "png",
     "count": 2, "interval_ms": 100, "label": "after"}
  ],
  "defaults": {"delay_ms": 100, "text_interval_ms": 0, "key_gap_ms": 30}
}
```

`keys`는 "순차 목록의 각 원소가 코드(chord)"인 2차원 배열이다(`k[]ctrl+shift+a b` → `[["ctrl","shift","a"],["b"]]`). `mods`는 심볼(`ctrl/shift/alt/meta/primary`)이며 backend가 OS 키코드로 해석한다. 캡처 op에 저장 경로가 없음에 유의 — 경로는 클라이언트 관심사다. `exec[]git status`는 `{"op": "exec", "argv": ["git", "status"], "shell": false, "noerr": false, "timeout_ms": 10000}`, `open[]Blender`는 `{"op": "open", "target": "Blender", "wait_ms": 0}`가 된다. 요청 body에서 이 IR은 `{"deadline_ms": N, "ir": {...}}`로 감싸진다(8.3).
