# gotto-hando — 컨셉 문서

> AI 에이전트가 shell에서 한 줄 문법으로 데스크톱(GUI)을 제어하게 하는 순수 CLI 도구.
> `gotto-hando <dest> <line>...` 한 번의 호출로 키 입력·마우스·윈도우 포커스·화면 캡처를 순차 실행한다.

이 문서는 `gunpowder-odyssey/tools/cua-batch`(Python, JSON 입력, SSH+Cua 기반 Windows 원격 제어)를 참조 구현으로 분석한 뒤,
그 기능 집합을 계승하면서 **JSON 없는 커스텀 문법**, **단일 정적 바이너리**, **IR + backend + transport 계층 분리**, **`--help` = 풀 매뉴얼**(플랫폼·원격 설정은 `--help-macos`/`--help-windows`/`--help-remote`로 분리)
원칙으로 재설계한 결과다.

> **단일 SoT(source of truth).** 규범(normative) 내용 — 문법, 커맨드 레퍼런스, 기본값, 제한, 출력 형식, exit code, 플랫폼 절차, IR JSON, ssh 래퍼 동작, 브릿지 프로토콜 — 은 `assets/help.txt`, `assets/help-macos.txt`, `assets/help-windows.txt`, `assets/help-remote.txt`가 **유일한 SoT**다. 이 문서는 배경·근거·아키텍처·로드맵만 다루며, 여기 적힌 문법/동작 서술이 help 텍스트와 다르면 **help 텍스트가 맞다**. 규범을 바꿀 때는 help 텍스트를 먼저 고친다(3장).

---

## 목차

1. [참조 구현(cua-batch) 분석 요약](#1-참조-구현cua-batch-분석-요약)
2. [제품 개요](#2-제품-개요)
3. [`--help` = 스펙 = 단일 SoT 원칙](#3---help--스펙--단일-sot-원칙)
4. [시퀀스 문법 — 설계 결정과 근거](#4-시퀀스-문법--설계-결정과-근거)
5. [커맨드 집합 — 설계 결정과 근거](#5-커맨드-집합--설계-결정과-근거)
6. [실행 모델 — 설계 결정과 근거](#6-실행-모델--설계-결정과-근거)
7. [CLI·출력 — 설계 결정과 근거](#7-cli출력--설계-결정과-근거)
8. [아키텍처: IR + backend + transport](#8-아키텍처-ir--backend--transport)
9. [플랫폼별 구현 노트 — 설계 근거](#9-플랫폼별-구현-노트--설계-근거)
10. [원격 실행과 보안 — 설계 근거](#10-원격-실행과-보안--설계-근거)
11. [cua-batch ↔ gotto-hando 매핑](#11-cua-batch--gotto-hando-매핑)
12. [리포 구조 초안](#12-리포-구조-초안)
13. [로드맵](#13-로드맵)
14. [Open questions](#14-open-questions)

규범 텍스트: [`assets/help.txt`](assets/help.txt) · [`assets/help-macos.txt`](assets/help-macos.txt) · [`assets/help-windows.txt`](assets/help-windows.txt) · [`assets/help-remote.txt`](assets/help-remote.txt)

---

## 1. 참조 구현(cua-batch) 분석 요약

### 1.1 파일별 역할

| 파일 | 역할 | gotto-hando에서의 대응 |
|---|---|---|
| `cua_batch.py` | argparse CLI. `batch`/`capture` 서브커맨드, `--json`/`--file`/`--validate`/`--summary`. `MANUAL` 문자열을 `--help` epilog로 출력 | `cmd/gotto-hando` + `assets`(help 텍스트 embed) |
| `schema.py` | 액션 union의 엄격한 스키마 검증(필수/선택 필드, 범위, 라벨 문자 집합, held 버튼 소유권 검사, 총 시간·캡처 수 예산) | `internal/ir` + `internal/syntax` 검증 단계 |
| `prepare.py` | `paste_file`을 클라이언트에서 읽어 `paste{text}`로 정규화 (UTF-8, 16384자/65536바이트 제한) | 파서의 `[f]` 플래그 처리 |
| `engine.py` | 순차 실행기. held 키/버튼 추적, 실패 시 역순 릴리즈, 최종 캡처, 타이밍 기록 | `internal/engine` |
| `backend.py` | Cua 드라이버(`cua-driver.exe call <tool>`)로 캡처·스크롤·클립보드, `Native`로 입력. preflight(물리 키 눌림, 좌표 범위, 키 이름, Cua 스키마) | `internal/backend` 인터페이스 |
| `native.py` | Windows `SendInput` 어댑터. 세션/데스크톱 잠금 확인, 확장키·키패드 스캔코드, 유니코드 텍스트, 폴리라인 드래그 보간 | `internal/backend/windows` |
| `transport.py` | SSH 1회로 부트스트랩(PowerShell EncodedCommand + stdin JSON) → 원격 임시 폴더 → `scp -r`로 캡처 회수 | `internal/transport/ssh` (ssh 래퍼: 시퀀스는 stdin, 결과·캡처는 stdout JSONL로 회수) |
| `worker.py` | 원격 인터랙티브 세션 워커. `started`/`cancel`/`result.json` 파일 프로토콜, 360s 데드라인, `FreeConsole` | `--bridge` 세션 브릿지(GUI 세션 상주, unix socket/named pipe)로 대체 |
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
- **fail-fast + 정리**: 액션 실패 시 이후 액션 중단 → held 전부 역순 release → 최종 캡처 시도. release 실패도 최종 캡처를 막지 않는다. (gotto-hando는 암묵 최종 캡처를 없애고 `--cap-on-error`로 옵트인한다 — 6.4, 11장 참조.)
- **클립보드 원자성**: 클립보드 쓰기 실패 시 Ctrl+V를 보내지 않는다(옛 내용이 붙여넣어지는 사고 방지). 키 down 실패 시 이미 누른 키는 반드시 release.
- **타이밍 기록**: 액션별 `started_s/completed_s/delay_completed_s`, 캡처별 `scheduled/started/completed/slippage`.
- **입력 수락 ≠ 애플리케이션 반영**: 결과에 `effect: unverified`를 명시. 확인은 캡처로.
- **최종 캡처 파일 경로만 출력, base64/자동 열기 없음**.

### 1.4 계층 구조에서 얻는 힌트

cua-batch는 이미 `schema(IR) → engine(시퀀서) → backend(OS 어댑터) → transport(원격)` 4층이 분리되어 있다. JSON 액션 union이 사실상 IR이며, `engine.execute(batch, backend, clock)`가 backend·clock을 주입받아 오프라인 테스트가 가능한 구조다. gotto-hando는 이 분리를 그대로 가져가되, (1) IR 앞에 **텍스트 파서** 층을 추가하고, (2) transport를 "SSH 1회 부트스트랩 + scp"에서 "ssh 래퍼(stdin/stdout 스트림) + GUI 세션 상주 브릿지"로 바꾼다.

### 1.5 테스트/README에서 드러난 함정 (모두 help 텍스트의 `CAVEAT:`에 반영)

| 함정 | 출처 | 반영 |
|---|---|---|
| Windows 유니코드 `SendInput`은 Blender 등 스캔코드 기반 앱에서 무시됨. 숫자 변환은 개별 키 이벤트로 | README, MANUAL | `txt` caveat, `k`로 대체 안내 |
| 키패드는 `KEYEVENTF_SCANCODE`(flag 8)로 NumLock과 무관하게 물리 위치 전송, 화살표/Home/End 등은 `EXTENDEDKEY` 필요 | `native.key`, `test_native_key_preserves_keypad_scan_and_release` | Windows backend 요구사항 |
| 마우스 이동은 `MOUSEEVENTF_ABSOLUTE\|VIRTUALDESK`로 정규화(0..65535), 가상 데스크톱 원점이 음수일 수 있음 | `native.move`, `bounds` | 좌표 공간 정의 |
| 워커가 활성 콘솔 세션(`WTSGetActiveConsoleSessionId`)이 아니거나 데스크톱이 `Default`가 아니면(잠김/UAC) 입력 불가 | `check_session` | RDP/잠금 caveat, `qinfo`의 `session` 필드 |
| SSH로 실행된 프로세스는 인터랙티브 세션이 아니므로 Cua `launch_app`으로 워커를 다시 띄우고 `FreeConsole`로 콘솔을 뗌 | transport BOOTSTRAP, worker | 사용자가 GUI 세션에서 `--bridge`를 상주시키고, ssh가 띄운 프로세스는 IR을 브릿지로 넘기는 설계로 해결 |
| 붙여넣기는 Return을 보내지 않지만 콘솔은 개행을 실행할 수 있음. Blender Console에는 한 줄 `exec(compile(...))`로 | README | `paste` caveat |
| 클립보드 쓰기 실패 시 붙여넣기 금지, 키 실패 시 소유 키 release | `test_paste.py` | engine 규칙 |
| `paste_file` 내용이 셸 커맨드 소스에 섞이면 안 됨(`$HOME`, backtick 등) | `test_transport_sends_file_contents_as_data_only` | 시퀀스·IR은 항상 데이터 채널(ssh stdin, 브릿지 소켓)로만 전달. 원격 커맨드라인은 고정 문자열 |
| 라벨에 `../` 금지, 캡처 파일명 검증, 원격 디렉터리 경로 검증 | schema, transport | 캡처 경로는 클라이언트가 결정 |
| 물리적으로 키/버튼이 눌린 상태에서 시작하면 거부 | `Backend.preflight` | preflight 규칙 |
| 프레임 캡처 타이밍은 best-effort, 첫 캡처 1s 지연 사례 | README 타이밍 증거 | `cap[n=]` caveat |
| `start_minimized` 등 드라이버 옵션이 성공 코드로도 실패 가능 | README | 응답 검사 로직(`check_response`)을 backend 결과에 반영 |
| 순간 캡처 66ms, 왕복 1.8s(SSH 부트스트랩 포함) | README | 원격 부트스트랩(PowerShell·워커 launch·scp) 제거: ssh 1회 + 상주 브릿지. ssh 접속 비용은 `ControlMaster`로 상쇄 |

---

## 2. 제품 개요

### 2.1 한 줄 정의

`gotto-hando`는 **non-MCP, 순수 CLI** 데스크톱 제어 바이너리다. 에이전트는 툴 프로토콜 없이 shell에서 다음처럼 호출한다.

```sh
gotto-hando local 'win[]Blender' 'k[c]a' 'txt[ms=20]hello' 'cap'
gotto-hando winbox -f steps.txt
printf '%s\n' 'm[]1024,133' 'c' 'cap' | gotto-hando winbox
```

- 첫 번째 인자 `<dest>`: 제어 대상 PC. `local`은 예약(현재 머신, in-process). 그 외는 `ssh`가 받는 목적지 그대로(`host`, `user@host`, `~/.ssh/config`의 Host 별칭) — gotto-hando 자체 설정 파일은 없다.
- 이후 인자 각각이 **한 줄 = 한 명령**인 시퀀스. stdin/파일도 지원.
- 한 번의 실행 안에서 상태 머신으로 동작: 선택된 윈도우, held 키/버튼, 지연 설정 등이 다음 줄로 이어진다.

### 2.2 cua-batch와 다른 점

| 항목 | cua-batch | gotto-hando |
|---|---|---|
| 입력 | JSON (`{"type":"key","keys":["ctrl","v"]}`) | 한 줄 문법 (`k[c]v`) |
| 언어/배포 | Python 3.10+, Cua 드라이버 의존 | Go 단일 정적 바이너리, 외부 의존 없음 |
| 플랫폼 | Windows 타깃(클라이언트는 Mac) | macOS + Windows 양쪽 타깃, 양쪽 클라이언트 |
| 원격 | 배치마다 SSH 부트스트랩 + 워커 launch + scp | `ssh <dest> gotto-hando local ...` 래핑(시퀀스 stdin, JSONL stdout) + GUI 세션 상주 `--bridge`. 네트워크 리스너 없음 |
| 윈도우 제어 | 없음(항상 foreground) | 윈도우 조회/포커스/윈도우 기준 좌표 |
| 캡처 | 전체 화면 PNG만 | 전체/디스플레이/윈도우/영역, 스케일, 포맷 |
| 임의 실행 | 없음(부트스트랩용 SSH+PowerShell은 내부 구현) | `exec`(명령 실행)·`open`(앱 실행) — 그 ssh 계정의 셸과 동등(5.4, 10장) |
| 도움말 | `--help` epilog | `--help` = 풀 매뉴얼 (사용법·문법·레퍼런스·caveat·예제). 플랫폼·원격 설정은 `--help-macos`/`--help-windows`/`--help-remote` (3장) |

### 2.3 언어 선택: Go (권장)

| 기준 | Go | Rust | 비고 |
|---|---|---|---|
| 단일 정적 바이너리 | 기본 | 기본 | 동률 |
| cross-compile | `GOOS/GOARCH`만으로 됨(cgo 없음) | `cross`/zig 등 필요 | Go 우세. macOS 네이티브 API는 `purego`의 dlopen/dlsym으로 cgo 없이 호출, Windows는 `x/sys/windows`(syscall) → **두 타깃을 한 호스트에서 `CGO_ENABLED=0`으로 크로스 빌드** |
| 데스크톱 자동화 생태계 | `purego`(cgo 없는 C 호출), `golang.org/x/sys/windows`; `robotgo`(cgo, 통합 패키지)는 기각 | `enigo`, `windows-rs`, `core-graphics` | 둘 다 충분. Go는 `x/sys/windows`로 `SendInput`/`user32` 직접 호출이 쉬움 |
| 프로세스 실행·IPC(ssh 자식 프로세스, unix socket, named pipe) | 표준 라이브러리(+`x/sys/windows` 파이프 ACL) | `tokio` 등 의존 | Go 우세 |
| 빌드 속도/개발 속도 | 빠름 | 느림 | Go 우세 |
| 안전성/성능 | 충분(입력 이벤트는 저부하) | 더 강함 | 이 용도엔 무의미 |

결정: **Go**. `robotgo`는 의존 트리가 크고(libpng, X11 헤더 등) 동작을 세밀히 제어하기 어려우므로, **플랫폼 API 직접 호출**을 기본안으로 한다 — macOS는 `github.com/ebitengine/purego`로 CoreGraphics/ApplicationServices/AppKit(objc 런타임 경유)를 dlopen하여 **cgo 없이**, Windows는 `x/sys/windows`로. 결과적으로 어느 호스트에서든 `CGO_ENABLED=0 GOOS=darwin|windows go build`가 된다(12장). 후보 API:

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

## 3. `--help` = 스펙 = 단일 SoT 원칙

에이전트는 사전 지식 없이 `gotto-hando --help` 한 번으로 **도구를 쓰는 데 필요한 모든 것**을 배워야 한다. 따라서 `--help`는 옵션 요약이 아니라 사용법·문법 스펙·전체 커맨드 레퍼런스·실행 모델·출력 형식·제한·caveat·예제를 담은 단일 문서이며, 그 텍스트가 곧 **스펙**이다.

### 3.1 SoT 규칙

- 규범 텍스트는 `assets/help.txt`, `assets/help-macos.txt`, `assets/help-windows.txt`, `assets/help-remote.txt` 네 파일뿐이다. `assets/assets.go`가 `//go:embed`로 바이너리에 포함하고, 각 `--help*` 플래그는 해당 문자열을 **그대로** 출력한다(가공 없음, exit 0).
- 이 문서(CONCEPT.md)는 배경·근거·아키텍처·로드맵만 다룬다. 여기 적힌 문법이나 동작 서술이 help 텍스트와 다르면 **help 텍스트가 맞다**. 규범을 바꾸려면 help 텍스트를 고치고, 근거가 바뀌었으면 이 문서를 고친다.
- 언어는 **영어**(확정; 에이전트 대상이므로 토큰 효율과 앱 문서 용어와의 일치가 우선). 형식은 UTF-8, 80컬럼 이내 래핑(표는 100컬럼까지 허용), 섹션 헤더는 `== NAME ==` 고정, 파일 끝 개행 하나.
- 네 텍스트는 서로 중복하지 않고 상호참조만 한다. 플랫폼 고유 함정은 `help.txt`에 한 줄 포인터만 두고 본문은 해당 플랫폼 텍스트에 둔다.

### 3.2 네 개의 도움말로 나눈 이유 (확정)

| 플래그 | 파일 | 언제 읽는가 |
|---|---|---|
| `--help` | `help.txt` | 항상. 도구를 처음 쓸 때 |
| `--help-macos` | `help-macos.txt` | `E_PERMISSION`/`E_SESSION`(exit 4)이나 `qinfo`의 `perms=...:missing`이 나올 때, macOS를 재빌드했을 때, macOS에 `--bridge`를 띄울 때(LaunchAgent) |
| `--help-windows` | `help-windows.txt` | `qinfo`의 `session=`이 `active`/`bridge`가 아닐 때, Windows에 `--bridge`를 띄울 때(작업 스케줄러), 관리자 권한 앱에 입력이 안 들어갈 때, `exec` 출력이 깨질 때 |
| `--help-remote` | `help-remote.txt` | `local`이 아닌 PC를 제어하려 할 때, exit 3(`E_CONNECT`)이나 원격 exit 4(`E_SESSION`)가 날 때. ssh 래퍼 동작과 브릿지 프로토콜(`== SESSION BRIDGE ==`)도 여기 |

- 플랫폼·원격 설정은 대부분의 호출에서 필요 없고 분량이 크므로 `--help`에서 제외해 opt-in으로 분리했다. 셋은 "확인 명령 → 판정 → 조치 → 안 되면 사용자에게 안내할 문구" 순서의 절차형이며, 사람이 클릭해야만 되는 단계(macOS TCC 토글, Windows SmartScreen "실행")는 **자동화 불가**라고 명시한다.
- `--help [topic]`(섹션만 출력하는 토픽 인자)은 **채택하지 않는다**. `--help`는 항상 전체를 출력하고 에이전트는 고정 헤딩을 `grep`한다. 토픽 인자는 "어떤 토픽이 있는지"를 또 배워야 하는 2단계 학습을 만든다.
- 가독성 기준: 각 섹션 첫 줄이 실행 가능한 예제, 표는 고정폭 텍스트, 함정은 `CAVEAT:` 접두어 + 권장 대안, 실패 조건은 exit code와 에러 코드로 정확히. 길이 제한은 없다.

### 3.3 드리프트 방지 테스트 계획

help 텍스트가 스펙이므로 "구현이 help와 다르다"는 곧 버그다. 다음 테스트를 `assets`와 각 패키지에 둔다.

1. **형식 lint** (`assets`): 네 파일 모두 UTF-8, 80컬럼(표·예제 줄은 100컬럼) 이내, 헤더가 `^== [A-Za-z0-9 ,/:()-]+ ==$` 형식(대문자 기본, 커맨드·플래그 이름만 소문자 허용), 파일 끝 개행 하나, 헤더 중복 없음.
2. **스펙 앵커 ↔ help 섹션 존재 검사**: `ai-docs/spec/*.md`의 각 앵커(`{#slug}`)는 자신이 근거로 삼는 help 파일과 섹션 이름을 명시한다(예: `help.txt#COMMANDS`). 테스트는 모든 앵커가 가리키는 섹션이 실제로 존재하는지, 그리고 `help.txt`의 필수 섹션 집합(SYNOPSIS, OPTIONS, QUICK START, SYNTAX, GRAMMAR, MODIFIERS, MODIFIER KEYS, COORDINATES, DURATIONS, TEXT ESCAPES, WINDOW SELECTORS, COMMANDS, KEY NAMES, EXECUTION, STATE MACHINE, ERROR POLICY, OUTPUT, JSONL, IR JSON, ERROR CODES, EXIT CODES, LIMITS, CAVEATS, SHELL QUOTING, DESTINATIONS, EXAMPLES, SEE ALSO)이 빠짐없이 있는지 검사한다.
3. **파서 커맨드 테이블 ↔ `== COMMANDS ==` 일치 검사** (`internal/syntax`): `== COMMANDS ==` 섹션에서 `^  <cmd>( \[|$)` 패턴으로 커맨드 이름을 추출해 파서의 커맨드 테이블과 **양방향** 집합 비교. 각 커맨드의 허용 모디파이어(대괄호 안의 flag/key 이름)도 파서 테이블과 비교한다.
4. **키 이름 ↔ `== KEY NAMES ==`**: 섹션의 토큰(별칭 `a|b` 분리, `f1-f24`·`numpad0-numpad9` 전개)과 키 테이블의 양방향 비교.
5. **에러/exit 코드 ↔ 열거형**: `== ERROR CODES ==`의 `E_*` 집합과 `backend` 에러 코드 열거형, `== EXIT CODES ==`의 0–5와 `output`의 exit 계산.
6. **제한 상수 ↔ `== LIMITS ==`**: `ir/limits.go`의 상수를 표의 숫자와 대조(줄 수, 64 KiB, 코드 8키, 틱 1..50, 캡처 121 등).
7. **예제 파싱**: `== EXAMPLES ==`, `== QUICK START ==`, `== COMMANDS ==`의 예제 줄(`'...'` 인용 인자와 들여쓴 bracket-form 줄)을 전부 `--check`로 통과시킨다.
8. **CLI 계약**: `--help*`가 파일과 바이트 동일한 출력 + exit 0, 다른 옵션·줄과 함께 줘도 동일.

---

## 4. 시퀀스 문법 — 설계 결정과 근거

규범: `assets/help.txt`의 `== SYNTAX ==`, `== GRAMMAR ==`, `== MODIFIERS ==`, `== MODIFIER KEYS ==`, `== COORDINATES ==`, `== DURATIONS ==`, `== TEXT ESCAPES ==`, `== WINDOW SELECTORS ==`, `== SHELL QUOTING ==`. 아래는 왜 그렇게 정했는가만 적는다.

### 4.1 bracket-form 필수, space-form 없음 (확정)

한 줄의 형태를 `<command>[<modifiers>]<payload>`와 `<command>` 둘로 제한하고 `m 1024,133` 같은 공백 구분 형태를 문법 오류로 만들었다. 파싱 규칙이 하나로 줄고, "공백 정확히 1개" 규칙과 텍스트 계열의 선행 공백 보존 caveat이 사라지며, payload 시작 위치가 항상 첫 `]` 직후로 명확하다. 비용은 줄당 2문자. 부수 효과로 payload가 있는 모든 줄에 `[`·`]`가 들어가므로 셸 인용이 **항상** 필요해졌고, help는 single quote/heredoc/`-f`를 권장한다.

### 4.2 모디파이어 표기

- 토큰에 `=`가 있으면 kv, 없으면 flag 묶음(`[cs]` = `[c,s]`). 짧은 줄을 위해 flag 결합을 허용하되 `[ms]`(flag m,s)와 `[ms=20]`(key ms)이 구분되도록 `=` 유무만으로 판정한다.
- value 안의 리스트는 `:` 구분(`rect=0:0:600:400`). `,`는 토큰 구분자이고 인용/이스케이프를 도입하지 않기로 했으므로 남는 문자 중 좌표와 어울리는 `:`를 골랐다. 리스트 값을 받는 key는 `rect=`뿐이며, 경로처럼 긴 값은 payload로 받는다.
- 알 수 없는/중복 flag·key는 오류(엄격 모드). 오타를 조용히 무시하면 에이전트가 원인을 찾을 수 없다.
- payload는 EOL까지 raw. 텍스트 계열(`txt paste clip`)만 앞뒤 공백 verbatim, 나머지는 trim. `[f]`는 경로이므로 텍스트 계열이라도 trim(확정).

### 4.3 `c` = Control, `p` = primary (확정)

`c`는 항상 문자 그대로 Control, `m`은 Meta(Cmd/Win), 플랫폼 기본 modifier는 `p`.

1. **예측 가능성**: 앱 문서의 "Ctrl+V"/"⌘V"를 그대로 옮기면 OS가 그 키를 받는다. 숨은 변환이 없다.
2. **macOS의 진짜 Control**: 터미널 `^C`, `^A`/`^E`, emacs 바인딩, `ctrl+space` 등은 Control이어야 한다. `c`를 primary로 매핑하면 macOS 터미널에서 `k[c]c`가 Cmd+C로 둔갑해 프로세스 중단이 조용히 실패한다.
3. **이식성은 `p`로**: 플랫폼을 모르고 쓰는 시퀀스는 `k[p]v`. `paste`는 내부적으로 `p+v`.
4. `qinfo`가 `primary=cmd|ctrl`을 출력하므로 언제든 확인 가능.

기각: "`c` = primary, Control은 별도 문자 `x`" — 사용자 예시와 Windows 관습에는 맞지만 2번 사고를 유발하고 `x`의 발견 가능성이 낮다. Win 키 전용 flag도 두지 않는다(Meta는 `m`뿐, `w`는 window 프레임 flag). 키 이름 `win`(=`meta`=`cmd`)은 payload에서 쓸 수 있다.

### 4.4 좌표

- **논리 좌표**(macOS points, Windows 물리 픽셀 + Per-Monitor-V2)를 쓰고, 기본 캡처(`scale=1`)를 이 좌표계와 1:1로 저장한다. 에이전트의 작업 루프가 "캡처를 보고 픽셀 좌표를 그대로 입력"이기 때문이며, Retina 다운스케일은 이 편의의 대가다(원본은 `scale=native`).
- 기준 프레임(`r`/`w`/`disp=`)은 모디파이어로 고르고 좌표 문자열은 항상 `x,y`. 프레임 모디파이어는 한 명령에 하나만, `%`는 `r`과 조합 불가(상대 이동에는 기준 크기가 없음) — 둘 다 문법 오류(확정).
- 범위 검사를 3단계로 나눈 이유: 오프라인 검증은 데스크톱 크기를 모르므로 숫자 형식만, 절대·`disp=` 좌표는 preflight에서 검사해 아무 입력 없이 exit 4, `r`/`w`/`%`는 실행 시점 상태에 의존하므로 그 줄 직전에 검사해 그 줄만 `err`.

### 4.5 시간·텍스트·선택자

- duration은 접미 없으면 **ms**. 대부분의 값(간격·정착)이 ms 단위라 줄이 짧아진다.
- 텍스트 이스케이프는 `\n \t \\ \uXXXX`만 인정하고 그 외 `\?`는 오류. Windows 경로(`C:\Users`)를 조용히 잘못 타이핑하는 사고를 막기 위해서다. `txt`에서 `\n`은 Return 키(문자가 아니라 키 입력이므로), `paste`/`clip`에서는 개행 문자. `[f]`는 이스케이프 없이 파일 내용 그대로.
- 윈도우 선택자는 `id:`/`pid:`/`app:`/타이틀 부분 일치, `r` flag면 정규식. 정규식 엔진은 Go 표준 RE2(백트래킹 없음, 입력 길이에 선형). 다중 일치는 z-order 최전면 + `matched=N` 경고 — 에이전트가 모호함을 알아차리되 실행은 막지 않는다.

---

## 5. 커맨드 집합 — 설계 결정과 근거

규범: `assets/help.txt`의 `== COMMANDS ==`, `== KEY NAMES ==`, `== LIMITS ==`. 커맨드별 모디파이어·기본값·동작·제한은 전부 거기 있다. help는 v1 완성 상태를 기술하며, 어느 마일스톤에서 구현되는지는 13장에만 표기한다.

### 5.1 이름과 분류

짧은 소문자 니모닉(`k kd ku txt m c md mu drag scroll clip paste win open exec cap sleep set`), 조회는 `q` 접두(`qinfo qdisp qmouse qwin qclip`). 한 줄에 여러 번 타이핑되는 것이므로 짧을수록 좋고, `q` 접두는 "부작용 없음"의 표지다.

### 5.2 키보드·마우스

- 키 이름은 **물리 키(US 레이아웃 위치)**. 문자를 넣는 수단은 `txt`뿐이다. 두 개념을 섞으면 비-US 레이아웃과 IME에서 결과를 예측할 수 없다.
- 코드(chord)는 모디파이어 flag를 **포함해** 최대 8키. OS의 동시 hold 한계 기준이며 flag를 제외하면 실제 hold 수가 한계를 넘을 수 있다(확정).
- `scroll`의 틱 수는 payload로만 받고 `n=`은 두지 않는다. `n=`은 다른 커맨드에서 "반복 횟수"이므로 의미를 하나로 유지한다(확정).
- `kd`/`ku`는 engine이 held 목록으로 추적하고, `ku`는 `kd`로 누른 키만 해제할 수 있다(검증 오류). 이미 held인 버튼으로 `c`/`md`/`drag`도 검증 오류 — cua-batch의 held 소유권 검사를 계승.

### 5.3 클립보드

`paste` = `clip` + 정착 대기 + `primary+v`. 클립보드 설정 실패 시 키를 보내지 않는다(옛 내용이 붙여넣어지는 사고 방지, cua-batch `test_paste`). Return은 보내지 않는다(콘솔이 개행을 실행하므로 에이전트가 캡처로 확인한 뒤 `k[]enter`). 클립보드 복원은 하지 않는다(14장 open question 6).

### 5.4 `open`·`exec` — 임의 실행 허용 (확정)

원격 실행은 곧 `ssh <dest>`이므로 사용자는 이미 그 계정의 원격 셸을 가진다. `exec`/`open`은 새 권한을 추가하지 않고 왕복(별도 `ssh` 호출)만 줄인다. 로컬도 동일 — 사용자가 자기 셸에서 실행하는 것과 같다. 따라서 allowlist를 두지 않는다. 보안 함의는 10장.

- `exec[shell]`은 **로그인 셸**(`$SHELL -lc`, 폴백 `/bin/zsh -lc`; Windows는 `cmd /C`). launchd/작업 스케줄러가 띄운 브릿지의 축소된 PATH를 로그인 셸의 rc가 복구하기 때문이다(확정). PowerShell은 `exec[]powershell ...`로 명시 호출.
- `shell` 없는 `exec`의 argv 분할은 파서가 플랫폼 무관 규칙(`"..."` 인용만)으로 한다. IR이 타깃 OS를 알기 전에(에이전트 머신에서, `--ir`로) 완성되어야 하기 때문.
- exit code ≠ 0은 기본 `err`(E_EXEC)로 fail-fast. `grep`/`diff`처럼 1이 정상인 명령을 위해 줄 단위 `noerr` flag를 둔다. 타임아웃(E_TIMEOUT)은 `noerr`로 완화되지 않는다(확정).
- Windows 출력은 콘솔 출력 코드페이지(콘솔이 없으면 ACP)로 디코드해 UTF-8로 변환하고 디코드 불가 바이트는 U+FFFD. `chcp 65001`을 강제하지 않는다(프로그램마다 무시하거나 깨지므로; 확정).
- `exec`·`open`은 GUI 입력이 아니므로 전역 기본 `delay`를 적용하지 않는다(명시 `d=`는 적용; 조회 커맨드와 같은 규칙, 확정). `timeout=` 상한 60s, 출력 각 64 KiB.

### 5.5 캡처

- 캡처는 **에이전트 머신의 로컬 파일**로 저장하고 경로만 출력한다(사용자에게 base64를 보이거나 자동으로 열지 않음). 에이전트는 자기 머신의 파일만 읽을 수 있고, 원격 경로는 무의미하다. 원격에서는 `--inline-captures`로 JSONL 안에 base64로 실려 오고 로컬 래퍼가 파일로 푼다(8.3).
- 자동 경로 `<out>/<NNNN>-<label>-<UTC timestamp>.png`: 실행당 카운터로 순서를, 라벨로 의미를, 타임스탬프로 실행 간 구분을 준다. `<out>` 우선순위는 `--out` > `$GOTTO_HANDO_OUT` > `$TMPDIR/gotto-hando/<dest>/<run-id>/`.
- 결과 줄에 `WxH`와 `origin=x,y`를 넣어 영역/윈도우 캡처에서도 픽셀→절대 좌표 환산이 되게 했다.
- v1 캡처는 PNG만, 커서 제외(OS 기본). `fmt=jpg`·`q=`·`cursor`는 help 텍스트에 없고 M4에서 추가 시 함께 들어간다(모디파이어를 미리 예약하지 않음).

### 5.6 제한

cua-batch의 예산(라벨 문자 집합, 캡처 120+1, 실행 시간 300s)을 계승하고, 시퀀스 텍스트와 IR이 스트림 하나로 통째로 전달되므로 메모리 상한을 위해 줄 수 1000·줄 길이 64 KiB·`[f]` 64 KiB를 추가했다. 원격에서는 `[f]` 내용이 이스케이프된 형태로 줄에 인라인되어 전송되므로 인라인된 줄도 줄 길이 한도를 따른다(8.3). 표 전체는 `== LIMITS ==`.

---

## 6. 실행 모델 — 설계 결정과 근거

규범: `assets/help.txt`의 `== EXECUTION ==`, `== STATE MACHINE ==`, `== ERROR POLICY ==`.

### 6.1 전체 검증 후 실행

cua-batch에서 계승. 파싱·정적 검증(exit 2)은 완전 오프라인이며 `--check`/`--ir`는 연결하지 않는다. 연결(exit 3) → preflight(exit 4) → 실행(exit 1)의 사다리는 "아무것도 실행되지 않았음"을 exit code만으로 알 수 있게 하기 위한 것이다. preflight가 검사하는 것(권한·세션·물리 키·절대 좌표·키 지원)은 모두 "입력을 하나라도 보내기 전에 알 수 있는" 실패다.

### 6.2 상태 머신과 held 추적

- 한 실행이 하나의 상태 머신(현재 윈도우, held, delay 설정, 캡처 카운터). 실행 간에는 아무것도 유지되지 않고 브릿지도 상태가 없다 — 브릿지를 죽이고 다시 띄워도 호출 측 동작이 바뀌지 않게.
- held 키/버튼은 backend가 아니라 **engine**이 추적한다. 종료(정상·실패·타임아웃·시그널·연결 끊김) 시 역순 해제는 engine의 한 곳에서만 일어난다.
- 현재 윈도우가 없으면 `w` 프레임은 OS 포커스 윈도우를 그때그때 조회한다. `win` 없이도 `c[w]`를 쓸 수 있게.

### 6.3 기본 지연 100ms (확정)

cua-batch의 330ms는 Blender 기준 보수치였다. 기본은 100ms로 내리고 조정은 `--delay`(우선순위 `--delay` > 100ms; 원격에도 그대로 전달)와 `set[delay=]`로 한다. PC별 기본값 파일은 두지 않는다 — gotto-hando는 설정 파일이 없다(8.4). 조회·`sleep`·`set`·`cap`·`exec`·`open`은 GUI 입력이 아니므로 기본 지연 대상에서 제외하되, 명시 `d=`는 적용한다(명시 > 기본; "제외" 규칙은 기본값에만 해당, 확정).

### 6.4 에러 정책

- **fail-fast 기본** + 이후 줄 `skip` 표기. `-k`는 전역 완화이고 실패한 줄이 잡은 키는 즉시 해제해 held가 꼬이지 않게 한다. `exec[noerr]`는 줄 단위 완화.
- **암묵 최종 캡처 제거**: cua-batch는 실패 시 자동 캡처했지만 gotto-hando는 명시적 `cap` 또는 `--cap-on-error` 옵트인. 캡처 시점은 에이전트가 정하는 것이 맞고, 자동 캡처는 결과 파싱을 복잡하게 한다.
- `qwin` 0개는 실패가 아니고 `win` 불일치는 실패다. 조회는 "없음"이 정보이고, `win`은 이후 줄의 전제이기 때문.
- **입력 수락 ≠ 앱 반영**: 결과 `ok`는 OS가 입력을 받았다는 뜻이며 확인은 `cap`으로. cua-batch의 `effect: unverified`를 문장으로 옮겼다.

### 6.5 데드라인과 연결 손실 (확정)

- `--timeout`은 원격 프로세스에 옵션으로 전달되고, 원격 프로세스는 이를 브릿지 요청의 `run.deadline_ms`로 넘겨 **실행하는 쪽(브릿지)이 동일 값을 강제**한다. 로컬만 타임아웃하면 원격 run(보간 이동·버스트·`win[wait=]`·`exec` 자식)이 계속 돌고 held가 남는다. 브릿지가 중단·해제·`E_TIMEOUT` 보고·정상 `done`을 하므로 타임아웃은 `state=unknown`이 아닌 **정상 종료 경로**(exit 1)다.
- 로컬 래퍼는 start 객체를 본 뒤 ssh가 `done` 없이 끝나거나 데드라인 + 유예 5s 안에 `done`이 없으면(이때 ssh를 kill) 연결 손실로 보고 exit 5 + `state=unknown`. 원격 프로세스는 ssh 채널 소실을 취소로 간주하고, 브릿지는 호출자 소켓이 끊기면 run을 중단해 held를 해제한다. exit 5는 "held가 남았을 수 있다"는 유일한 경우이므로 별도 코드다. start 객체 이전의 모든 실패(ssh 접속 실패, `command not found`, 원격 조기 종료)는 exit 3 — "아무것도 실행되지 않음" 보장.

---

## 7. CLI·출력 — 설계 결정과 근거

규범: `assets/help.txt`의 `== SYNOPSIS ==`, `== OPTIONS ==`, `== OUTPUT ==`, `== JSONL ==`, `== IR JSON ==`, `== ERROR CODES ==`, `== EXIT CODES ==`, `== DESTINATIONS ==`.

### 7.1 호출 형태

- 첫 위치 인자가 `<dest>`, `local`은 예약. 이후 인자 각각이 한 줄. 옵션은 `-`로 시작하고 줄은 소문자·`#`·공백으로 시작하므로 위치에 관계없이 구분된다.
- `-f`와 argv 줄은 **혼용 금지**(exit 2). 결과의 줄 번호가 단일 소스를 가리켜야 에이전트가 자기 입력과 대조할 수 있다(확정).
- argv 줄도 stdin도 없고 TTY면 `qinfo`를 실행한다. "일단 실행해 보는" 첫 접촉에서 가장 유용한 정보가 연결·권한·세션 상태이기 때문.
- `--request-perms`는 `local|<dest>` 뒤에 붙는다. 프롬프트가 떠야 하는 머신(로컬 자신 vs 원격 Mac의 브릿지)을 `<dest>`가 결정하므로 위치 인자와 같은 자리다. `--help*`와 `--bridge`는 `<dest>` 없이 단독.
- 원격 실행 옵션의 분류: 실행 의미에 관여하는 것(`-q --delay --timeout -k --cap-on-error --request-perms`)은 원격 프로세스에 그대로 전달하고, 로컬 파일 시스템·ssh 자체에 관한 것(`--out --remote-bin`)은 로컬에서만 해석한다. `--jsonl --inline-captures`는 래퍼가 항상 원격에 붙이는 내부 형식 옵션이지만 `local`에서도 쓸 수 있는 정규 옵션으로 문서화한다(숨은 플래그를 두지 않기 위해). `--check`/`--ir`은 로컬에서 끝나고 ssh를 띄우지 않는다.

### 7.2 stdout 형식

- 한 op당 한 줄, 첫 두 필드(`<n> <status>`) 고정. `grep`/`awk`로 바로 걸러진다. 다중 라인 결과는 두 칸 들여쓰기 + TAB 구분으로 op 줄과 구분된다.
- `<n>`은 주석·빈 줄을 포함해 센 **입력 줄 번호**. 에이전트가 자기 입력과 1:1 대조.
- 경고·진단은 stderr. stdout에는 정해진 형식만 — 파싱 안정성.
- 마지막 줄 `done ...`은 항상 출력. exit 5일 때만 `state=unknown`.
- `--jsonl`은 같은 정보의 JSON Lines. 캡처는 어느 형식에서도 base64를 stdout에 내지 않고 경로만(cua-batch 계승).

### 7.3 에러 코드와 exit code

에러 코드는 고정 열거(`E_SYNTAX … E_UNKNOWN`)로 결과 줄 끝 `(<E_CODE>)`에 붙는다. 메시지는 사람용, 코드는 분기용. exit code 사다리(0 성공 / 1 일부 실행 후 실패 / 2·3·4 아무것도 실행 안 됨 / 5 상태 불명)는 "입력이 타깃에 갔는가"와 "held가 남았을 수 있는가"를 exit code만으로 답하도록 설계했다. `--timeout` 초과는 1이지 5가 아니다(6.5).

### 7.4 `--request-perms` (macOS)

`qinfo`는 프롬프트 없이 `perms=`만 보고한다(`AXIsProcessTrusted`, `CGPreflightScreenCaptureAccess`). 프롬프트를 띄우는 별도 진입점이 필요했고, 원격 Mac에서는 프롬프트가 **GUI 세션의 프로세스**에서 호출되어야 뜨므로 ssh가 띄운 프로세스는 이 요청도 브릿지에 넘긴다(브릿지 프로토콜의 `request_perms` 요청, 8.3). 브릿지가 없으면 `E_SESSION` exit 4. 프롬프트는 목록에 추가만 하고 토글은 사람이 켜야 한다는 한계는 help-macos에 명시. 절차와 exit code는 help.txt `== OPTIONS ==`와 help-macos.

---

## 8. 아키텍처: IR + backend + transport

```
┌──────── agent machine: gotto-hando <dest> ... ────────────────────────┐
│ cli ─► syntax.Parse ─► ir.Validate ─► [f] inline ─► transport 선택     │
│                                                       │               │
│            ┌──────────────────────────────────────────┤               │
│            ▼ dest=local (GUI 터미널)                    ▼ dest=ssh 목적지 │
│      engine.Run ─► backend.Local              transport/ssh:          │
│      (darwin|windows, in-process)             spawn `ssh <dest>       │
│                                                <remote-bin> local     │
│                                                --jsonl                │
│                                                --inline-captures      │
│                                                <fwd opts> -f -`       │
│                                               stdin ◄ 시퀀스 텍스트      │
│                                               stdout ► JSONL(base64)  │
│                                               → 로컬 파일로 캡처 저장      │
└───────────────────────────────────────────────────────┼───────────────┘
                                                     sshd │ (인증·암호화는 ssh)
┌──────── remote machine ────────────────────────────────▼───────────────┐
│ gotto-hando local  (sshd가 띄움: SSH_CONNECTION 감지 → 원격 모드)         │
│   syntax.Parse ─► ir.Validate ─► transport/local ─► bridge client      │
│                                                         │ unix socket /│
│                                                         │ named pipe   │
│ gotto-hando --bridge  (사용자가 GUI 세션에서 상주시킴)         ▼             │
│   bridge.Serve ─► engine.Run ─► backend.Local (darwin|windows)         │
│   IR JSON(+run) in → JSONL(--inline-captures) out, 한 번에 한 run        │
└────────────────────────────────────────────────────────────────────────┘
```

cua-batch의 `schema(IR) → engine → backend → transport` 4층을 그대로 가져오되, IR 앞에 텍스트 파서 층을 추가하고 transport를 **ssh 래퍼 + GUI 세션 브릿지**로 바꾼다(1.4). 규범은 `assets/help.txt`의 `== DESTINATIONS ==`/`== IR JSON ==`과 `assets/help-remote.txt`의 `== HOW IT WORKS ==`/`== SESSION BRIDGE ==`에 있다. 자체 와이어 프로토콜(HTTP 엔드포인트)은 없다 — 원격 구간은 ssh의 stdin/stdout, 머신 내 구간은 소켓 위의 IR JSON/JSONL이다.

### 8.1 파서 → IR

- IR은 **타입이 있는 op 리스트**로 JSON 직렬화 가능(`--ir`). cua-batch의 JSON 액션 union이 원형이며 새 문법은 그 위의 표기법일 뿐이다.
- 각 op는 `line`과 `src`를 보존해 에러 보고에 쓴다.
- 모디파이어 키 `p`(primary)와 키 이름은 IR에 **심볼 그대로** 남기고 backend가 OS에 맞춰 해석한다. 에이전트 머신이 타깃 OS를 모르는 채로 IR을 만들 수 있게 하기 위함이다(`--ir`, `--check`는 ssh 없이 끝난다).
- `[f]` 파일은 **에이전트 머신**의 파서 단계에서 읽는다(cua-batch `prepare`와 동일). `local`에서는 IR에 인라인하고, `<dest>`로 보낼 때는 시퀀스 텍스트 자체를 다시 써서 보낸다 — `txt[f]`/`paste[f]`/`clip[f]`는 이스케이프된 내용을 인라인한 `txt[]`/`paste[]`/`clip[]`로, `qclip[f]<path>`는 `qclip[]`로 바꾸고 돌아온 텍스트를 로컬 `<path>`에 쓴다. 원격 프로세스와 브릿지는 시퀀스 처리를 위해 사용자의 파일을 읽거나 쓰지 않는다(브릿지 로그 제외). `exec`/`open`의 자식 프로세스는 이 원칙 밖 — 사용자의 명령이다.
- `exec`의 argv 분할은 파서가 한다(플랫폼 무관). `noerr`는 engine이 결과 상태를 정할 때만 본다(backend는 exit code를 그대로 돌려줌).
- 캡처 op와 `qclip[f]`의 저장 경로는 IR에 넣지 않는다(에이전트 머신의 관심사).
- 스키마 버전 `v`로 원격/브릿지 간 버전 불일치를 감지한다(양 머신은 같은 버전을 써야 한다).
- `keys`는 "순차 목록의 각 원소가 코드"인 2차원 배열(`k[]ctrl+shift+a b` → `[["ctrl","shift","a"],["b"]]`).

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
    Exec(ctx, req ir.ExecReq) (ExecResult, error)  // os/exec 공통 구현. ctx 데드라인 초과 시 kill. 출력 64 KiB 캡.
                                                   // shell=true: darwin `$SHELL -lc`, windows `cmd /C` + 코드페이지→UTF-8
}
```

- `click`, `drag`, `key` 코드, 반복, 보간, held 추적, 지연은 **engine**이 backend 원시 호출을 조합해 구현한다. backend는 가능한 한 얇게.
- 플랫폼 구현은 빌드 태그로 분리. 테스트용 `fake` backend는 이벤트를 기록해 engine 계약 테스트(cua-batch `Fake` 이식)에 쓴다.

### 8.3 transport: local / ssh / bridge

| 계층 | 구현 |
|---|---|
| `transport/local` | `dest=local`이고 GUI 세션의 터미널에서 호출된 경우: engine이 backend.Local을 직접 호출. 네트워크·소켓 없음 |
| `transport/ssh` | `dest≠local`: `ssh <dest> <remote-bin> local --jsonl --inline-captures <fwd opts> -f -`를 자식 프로세스로 띄우고, 시퀀스 텍스트를 stdin에, JSONL을 stdout에서 읽어 로컬 형식으로 변환·캡처를 파일로 저장. 인증·암호화·호스트키·점프호스트는 전부 ssh config |
| `bridge`(client) | `local`이 자신이 원격 세션임을 감지한 경우(어느 플랫폼이든 `SSH_CONNECTION`/`SSH_TTY` 존재; Windows는 추가로 프로세스 세션 ≠ 활성 콘솔 세션 또는 WinSta0 접근 불가): IR JSON + `run` 옵션을 같은 머신·같은 사용자의 소켓으로 보내고 JSONL을 중계. 브릿지가 없으면 preflight `E_SESSION` exit 4 |
| `bridge`(server, `--bridge`) | 사용자가 GUI 세션에서 상주시키는 같은 바이너리. macOS unix socket `$HOME/Library/Application Support/gotto-hando/bridge.sock`(0600/0700), Windows named pipe `\\.\pipe\gotto-hando-<username>`(현재 사용자 ACL). 한 번에 한 run, IR in → JSONL out, 호출자 단절 = 취소. TCC(macOS)와 인터랙티브 데스크톱(Windows)을 가진 실제 실행 주체 |

설계 결정:

- **원격 = ssh 래핑.** 이미 모든 원격 PC에 sshd가 있고(cua-batch도 SSH 전제) ssh가 인증·암호화·키·점프호스트·포트 포워딩을 전부 해결한다. 자체 HTTP 서버·토큰·터널·프로파일 파일은 이 위에 얹는 중복 계층이었다(10장의 기각 대안). 대신 필요한 것은 "원격 커맨드라인은 고정 문자열, 데이터는 stdin/stdout"이라는 규칙 하나 — 시퀀스가 원격 셸을 거치지 않으므로 인용 문제도 로컬 셸에만 남는다.
- **로컬에서 먼저 파싱·검증하고 `[f]`를 인라인한 뒤 원격에서 다시 파싱한다.** exit 2("아무것도 전송되지 않음")를 로컬에서 보장하고, `f`="에이전트 머신의 파일"이라는 의미를 원격에서도 지키기 위해서다(8.1). 원격 재파싱은 같은 버전이면 같은 결과이고, 원격 exit 2는 버전 불일치 신호다.
- **캡처는 JSONL 객체에 base64 인라인**(`--inline-captures`) → 로컬 래퍼가 디코드해 파일로 저장. 별도 scp/SFTP 왕복이나 원격 임시 폴더를 없애고, 단일 스트림이면 취소/오류 처리가 단순하다. ssh 압축·33% 오버헤드는 LAN에서 무시 가능. 버스트(`n=120`)가 문제가 되면 청킹 검토(14장). 같은 이유로 `exec` 출력(각 ≤ 64 KiB)과 `qclip` 텍스트도 인라인.
- **원격 머신에 아무것도 남기지 않는다**(cua-batch의 "원격 증거 폴더" 폐기). 에이전트 프롬프트에 포함된 텍스트·이미지가 원격에 남지 않는 것이 보안상 낫다. 브릿지 로그는 run 단위 한 줄(내용·이미지 없음).
- **브릿지는 ssh가 띄운 프로세스가 감지 즉시 반드시 경유한다**(폴백이 아님). 세 가지 이유: (1) Windows Session 0에서는 데스크톱 접근이 원천 불가, (2) macOS TCC 권한은 GUI 세션의 프로세스(=브릿지)에 안정적으로 귀속되고 프롬프트도 그곳에서만 뜬다, (3) **consent gate** — 브릿지 실행 = 사용자가 원격 제어를 허용한 상태, 종료 = 즉시 불가(kill switch). GUI 터미널에서 직접 호출한 `local`은 어느 플랫폼에서도 브릿지를 거치지 않는다(브릿지 유무와 무관).
- 브릿지 프로토콜은 IR JSON 한 문서(`--ir` 출력 + `run` 객체: `deadline_ms keep_going quiet cap_on_error`; 또는 `request_perms`) in → 기존 JSONL out. 새 인코딩을 만들지 않고 이미 규범인 두 형식을 재사용한다. `--request-perms`도 같은 경로(7.4).
- 결과는 op 완료 즉시 스트리밍. 브릿지는 동시에 하나의 run만(두 번째 호출자는 대기). 호출자 단절 = 취소.
- `--timeout`은 원격 옵션 → 브릿지 `run.deadline_ms`로 전달돼 실행 주체가 강제하고, 로컬 래퍼는 +5s 후 ssh를 kill(6.5).

### 8.4 목적지(`<dest>`)

gotto-hando는 설정 파일을 두지 않는다. `<dest>`는 `ssh`가 받는 문자열 그대로이고, 호스트 별칭·사용자·포트·키·점프호스트·`ControlMaster`는 전부 `~/.ssh/config`의 일이다. 유일한 자체 옵션은 `--remote-bin`(원격 바이너리 경로; sshd의 비대화형 PATH가 인터랙티브 PATH와 다르기 때문에 필요)이다. `delay`·`out` 같은 PC별 기본값 파일은 없고 CLI 옵션과 환경변수(`$GOTTO_HANDO_OUT`)만 있다. `local`은 예약어이며 ssh 별칭으로 정의해도 in-process가 우선한다.

---

## 9. 플랫폼별 구현 노트 — 설계 근거

사용자·에이전트에게 보이는 절차와 caveat은 `assets/help-macos.txt`, `assets/help-windows.txt`에 있다. 여기는 API 선택과 그 근거만.

### 9.1 macOS (darwin, purego — cgo 없음)

- **바인딩 방식**: `github.com/ebitengine/purego`로 `CoreGraphics`/`ApplicationServices`/`AppKit`(+`libobjc`의 `objc_msgSend`)을 dlopen/dlsym해 호출한다. cgo가 없으므로 macOS 호스트 없이도 `GOOS=darwin GOARCH=arm64|amd64 CGO_ENABLED=0`로 크로스 빌드되고, Windows 타깃과 같은 호스트에서 릴리스를 만든다(12장). 비용: 구조체 by-value 인자(`CGPoint`, `CGRect`)는 purego의 구조체 지원 범위 안에서만 쓰고, Objective-C 호출은 `objc_msgSend` 시그니처를 직접 맞춘다.

- **입력**: `CGEventPost(kCGHIDEventTap)`. 키는 `kVK_*` 가상 키코드(US 레이아웃 위치). 모디파이어는 별도 keydown 이벤트 **와** 이후 이벤트의 `CGEventFlags` 둘 다 설정(앱에 따라 flags만 보는 경우가 있음). 텍스트는 `CGEventKeyboardSetUnicodeString`로 keydown/keyup 쌍 — 대부분의 앱에서 IME를 우회. 더블클릭은 `kCGMouseEventClickState`를 명시해야 한다. 스크롤은 `CGEventCreateScrollWheelEvent(kCGScrollEventUnitLine)`.
- **좌표**: CGEvent/AX는 좌상단 원점, `NSScreen`은 좌하단 — 구현 시 혼동 주의. 주 디스플레이 좌상단 = (0,0).
- **윈도우**: 목록은 `CGWindowListCopyWindowInfo(OnScreenOnly|ExcludeDesktopElements)`, layer 0만. 포커스는 `NSRunningApplication.activate` + AX(`kAXMainAttribute`, `AXRaise`, `kAXMinimizedAttribute=false`).
- **캡처**: `ScreenCaptureKit`(12.3+, `SCScreenshotManager`는 14+)이 정식, 폴백 `CGWindowListCreateImage`(14에서 deprecated), 최후 `/usr/sbin/screencapture`. Retina 원본은 2x이므로 기본 `scale=1`은 다운샘플(4.4).
- **권한(TCC)과 서명 identity**: TCC는 코드 서명 identity에 권한을 묶는다. Go 링커의 ad-hoc 서명은 designated requirement가 cdhash라 빌드마다 바뀌고 권한이 소실된다. 그래서 **자체 서명 코드 서명 인증서(`gotto-hando-dev`)로 재서명**을 표준 빌드 단계로 둔다(`scripts/`). 공증은 불필요(유료 프로그램). 단 **책임 프로세스 규칙** 때문에 터미널에서 실행하는 로컬 사용 — 그리고 터미널 창에서 띄운 `--bridge` — 은 권한이 터미널 앱에 귀속되어 재빌드와 무관하다 — 서명 문제는 LaunchAgent로 띄우는 브릿지에서만 부각된다. 이 두 사실이 help-macos의 구조(LOCAL BUILD / RESPONSIBLE PROCESS / STABLE SIGNING IDENTITY / SESSION BRIDGE FOR SSH)를 결정했다.
- **원격 권한 프롬프트**: sshd가 띄운 프로세스에는 TCC 프롬프트가 뜨지 않고 권한도 귀속되지 않는다. 그래서 ssh가 띄운 `local`은 `--request-perms`도 브릿지에 넘기고, 브릿지가 자기 프로세스에서 `AXIsProcessTrustedWithOptions(prompt=true)`·`CGRequestScreenCaptureAccess()`를 호출해 프롬프트를 그 Mac 화면에 띄운다(7.4). 토글은 여전히 사람이 켜야 하고 화면 기록은 브릿지 재시작 후 반영 — 자동화 한계를 help에 명시한다.
- **GUI 세션 검출**: sshd가 띄운 프로세스는 Aqua bootstrap namespace 밖이라 CGEvent가 무시되고 캡처가 빈 이미지가 된다. `SSH_CONNECTION`/`SSH_TTY`로 원격 모드를 판정해 브릿지로 넘기고(8.3), 브릿지 자신은 `CGSessionCopyCurrentDictionary`로 잠김/콘솔 소유 여부를 `session=`으로 보고하고 preflight에서 막는다. Secure Event Input은 `E_INPUT` 힌트.
- **launchd PATH**: LaunchAgent가 띄운 브릿지의 PATH는 축소되어 있다. `exec[shell]`이 로그인 셸(`$SHELL -lc`)을 쓰므로 완화되고, `exec[]`는 절대 경로 권장. 별개로 sshd의 비대화형 PATH(`/usr/bin:/bin:/usr/sbin:/sbin` + `~/.zshenv`)에서 gotto-hando 자체를 찾는 문제는 `--remote-bin`으로 푼다(8.4).

### 9.2 Windows (cgo 없음, `golang.org/x/sys/windows`)

- **DPI**: 시작 시 `SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)` + 매니페스트. 좌표·캡처 모두 물리 픽셀이므로 논리 좌표 = 물리 픽셀(4.4).
- **입력**: `SendInput`. cua-batch `native.py` 규칙 계승: 키패드는 `KEYEVENTF_SCANCODE`로 물리 위치, 화살표/Ins/Del/Home/End/PgUp/PgDn/Win/우측 Ctrl·Alt/NumEnter는 `EXTENDEDKEY`, 모든 키에 `MapVirtualKeyW` 스캔코드 동봉(DirectInput 앱). 텍스트는 `KEYEVENTF_UNICODE`(UTF-16 단위). 마우스는 가상 화면 0..65535 정규화 + `VIRTUALDESK`.
- **포커스**: `SetForegroundWindow`는 호출 프로세스가 전면이 아니면 거부된다. (1) `AttachThreadInput` 후 `SetForegroundWindow`+`BringWindowToTop`, (2) 실패 시 합성 Alt 탭 후 재시도, `ShowWindow(SW_RESTORE)`. `GetForegroundWindow`로 검증 후 실패면 `E_NOWINDOW`.
- **윈도우 목록**: `EnumWindows` + visible + `DWMWA_CLOAKED` 제외 + 타이틀 비어있지 않음 + 툴 윈도우 제외. 프레임은 `DWMWA_EXTENDED_FRAME_BOUNDS`(그림자 제외) — 캡처 좌표와 일치시키기 위해.
- **캡처**: GDI `BitBlt(CAPTUREBLT)`, 윈도우는 `PrintWindow(PW_RENDERFULLCONTENT)`(가려져도 가능, 일부 GPU 앱은 검정), 선택적 DXGI Desktop Duplication(M4).
- **세션**: `ProcessIdToSessionId(self) == WTSGetActiveConsoleSessionId()` 이고 `OpenInputDesktop` 이름이 `Default`여야 입력 가능. OpenSSH 서버가 띄운 프로세스는 세션 0이라 절대 불가하므로, `local`은 `SSH_CONNECTION`/`SSH_TTY` 또는 세션 불일치/WinSta0 접근 불가를 보면 IR을 named pipe `\\.\pipe\gotto-hando-<username>`(현재 사용자 ACL)로 `--bridge`에 넘긴다. 브릿지는 작업 스케줄러 "사용자가 로그온한 경우에만"으로 로그온 시 띄운다 — help-windows의 구조를 결정한 사실. 관리자 권한 앱을 다루려면 브릿지를 `/RL HIGHEST`로.
- **콘솔**: 별도 `windowsgui` 빌드 대신 같은 바이너리의 `--bridge`가 작업 스케줄러에서 콘솔 없이 뜨고 로그를 `%LOCALAPPDATA%\gotto-hando\bridge.log`로 쓴다. 바이너리 하나를 유지하기 위해.
- **UIPI**: 관리자 권한 앱은 비승격 프로세스의 `SendInput`을 조용히 무시하고 `GetLastError`로 구분되지 않는다. 감지 불가이므로 `qinfo`에 `elevated=0|1`을 보고하고 help-windows로 안내(14장 open question 7).
- **`exec` 인코딩**: `cmd /C` 출력은 OEM 코드페이지일 수 있다. `GetConsoleOutputCP()`(콘솔 없으면 `GetACP()`)로 `MultiByteToWideChar` 변환, 실패 바이트 U+FFFD. `chcp 65001` 강제는 하지 않는다(5.4).
- **물리 입력 검사**: preflight에서 `GetAsyncKeyState`로 모디파이어/버튼이 눌려 있으면 거부(cua-batch preflight 계승).

### 9.3 공통

키 이름은 물리 키이므로 비-US 레이아웃에서 `k[]a`가 다른 문자를 낼 수 있고, IME 활성 시 `k` 알파벳은 조합에 들어간다 — 문자 입력은 `txt`. 유니코드 주입이 무시되는 앱(Blender, 일부 게임, Win32 콘솔)은 `k` 또는 `paste`. 모두 help의 CAVEAT.

---

## 10. 원격 실행과 보안 — 설계 근거

절차(원격 설치, 브릿지 등록, 트러블슈팅)와 브릿지 프로토콜은 `assets/help-remote.txt`와 플랫폼 help. 여기는 원칙과 근거.

1. **gotto-hando는 어떤 네트워크 리스너도 열지 않는다**(확정). 원격 구간은 전부 `ssh`다: 인증(키·에이전트·2FA)·암호화·호스트키 검증·점프호스트·포트는 sshd와 `~/.ssh/config`가 이미 제공하고 운영자가 이미 관리한다. 자체 서버·토큰·바인드 주소를 두면 "잘못 열린 포트 = 원격 셸"이라는 새 공격면과 새 설정 파일이 생기는데, 그것으로 얻는 것이 없다.
2. **원격 실행 권한 = 그 계정으로 sshd가 허용하는 것**, 그 이상도 이하도 아니다. `exec`·`open`은 그 계정의 셸과 동등하므로 새 권한을 추가하지 않는다(5.4). allowlist는 두지 않는다(ssh 셸에 allowlist가 없는 것과 같은 이유). 원격 커맨드라인은 고정 문자열이고 시퀀스는 stdin으로만 가므로, 시퀀스 내용이 원격 셸에 해석되는 경로가 없다(cua-batch `test_transport_sends_file_contents_as_data_only` 계승).
3. **브릿지 = consent gate + kill switch.** ssh가 띄운 프로세스는 데스크톱을 직접 건드릴 수 없고(Windows Session 0, macOS TCC/Aqua namespace) 반드시 사용자가 GUI 세션에서 상주시킨 `--bridge`를 거친다. 따라서 "ssh 계정이 있다"만으로는 데스크톱 제어가 되지 않고, 사용자가 브릿지를 켜 둔 동안에만 된다. 브릿지를 끄면 즉시 불가. 브릿지 엔드포인트는 같은 머신·같은 사용자만 접근 가능(unix socket 0600/디렉터리 0700, named pipe 현재 사용자 ACL)하고 네트워크 포트를 열지 않는다. 다중 사용자 호스트에서 다른 로컬 사용자는 그 사용자의 브릿지에 닿을 수 없다.
4. **원격에 아무것도 남기지 않는다.** 원격 프로세스와 브릿지는 시퀀스·캡처·`exec` 출력을 메모리에서만 처리하고 스트림으로 돌려준 뒤 폐기한다. 파일은 에이전트 머신에만 생긴다. 브릿지 로그는 run 단위 메타데이터 한 줄. `exec`/`open`의 자식 프로세스는 사용자의 명령이며 이 원칙의 대상이 아니다.
5. 브릿지는 동시에 하나의 run만. 호출자(ssh가 띄운 프로세스)가 끊기면 run을 취소하고 held를 해제하며 `exec` 자식도 kill. `run.deadline_ms`를 강제한다(6.5).

**기각한 대안(역사적 기록).** 초기 설계는 같은 바이너리를 `--serve <port> [--token]`로 `127.0.0.1`에만 바인드하는 HTTP/1.1 서버로 띄우고, 에이전트 머신에서 `ssh -L`로 터널을 열어 `profiles.toml`의 loopback 프로파일로 접속하며, 결과를 NDJSON으로 스트리밍하는 구조였다. 기각 이유: (a) 인증·암호화는 이미 SSH가 제공하므로 토큰·loopback 검사·터널 유지(autossh)는 전부 중복 운영 부담이었다; (b) 상주 서버가 실제로 해결하던 문제는 "Windows Session 0에서는 데스크톱을 못 만진다"와 "macOS TCC는 GUI 세션 프로세스에만 붙는다" 둘뿐인데, 이는 네트워크 서버가 아니라 **같은 머신 안의 사용자 소켓 하나**(브릿지)로 충분하다; (c) 프로파일 파일은 ssh config와 중복이었다. 따라서 네트워크 계층을 통째로 ssh에 위임하고 서버를 브릿지로 축소했다.

플랫폼 고유 함정(RDP 잠금, 세션 0, UAC, UIPI, SmartScreen, sshd의 축소된 PATH, 재빌드 후 권한 소실, quarantine, launchd PATH 축소)은 각 플랫폼 help의 `CAVEAT:`와 절차 섹션에 배분되어 있고, ssh·브릿지 관련 exit 3/4/5는 help-remote `== TROUBLESHOOTING ==`에 있다.

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
| `CUA_SSH_HOST`, `CUA_REMOTE_PYTHON`, `CUA_DRIVER` | `<dest>`(ssh config Host 별칭) + `--remote-bin` | 자체 설정 파일 없음 |
| `CUA_OUTPUT_DIR` | `--out` / `$GOTTO_HANDO_OUT` | |
| 원격 임시 폴더 + scp | ssh stdout의 JSONL(base64 인라인) + 에이전트 머신 로컬 저장 | |
| SSH+PowerShell 부트스트랩으로 워커 launch | 사용자가 GUI 세션에 상주시킨 `--bridge` | 자동 launch 없음: 브릿지 실행이 consent gate(10장) |

---

## 12. 리포 구조 초안

```
gotto-hando/
├── go.mod                      # module github.com/kang-sw/gotto-hando
├── CONCEPT.md                  # 이 문서(배경·근거·아키텍처·로드맵)
├── assets/                     # 규범 텍스트(SoT): help.txt help-macos.txt help-windows.txt help-remote.txt
│   └── assets.go               # //go:embed → assets.Help / HelpMacos / HelpWindows / HelpRemote
├── cmd/gotto-hando/main.go     # 진입점: 옵션 파싱, <dest> 판정, --bridge/--help 분기
├── internal/
│   ├── syntax/                 # lexer.go parser.go escape.go coord.go duration.go selector.go inline.go([f] 인라인 재작성) + golden tests
│   ├── ir/                     # ops.go (타입), json.go, validate.go, limits.go, version.go
│   ├── engine/                 # run.go (시퀀서), held.go, drag.go, timing.go, fake_backend_test.go
│   ├── backend/
│   │   ├── backend.go          # Backend 인터페이스, Info/Window/Image/ExecResult 타입, 에러 코드
│   │   ├── exec.go             # exec 공통 구현(os/exec, 64 KiB 출력 캡, ctx 데드라인 kill); exec_darwin.go(로그인 셸), exec_windows.go(cmd /C, 코드페이지→UTF-8)
│   │   ├── darwin/             # purego(cgo 없음): dl.go(dlopen/objc_msgSend) input.go(CGEvent) window.go(AX/CGWindowList) capture.go(SCK/CG) clipboard.go session.go open.go
│   │   ├── windows/            # sendinput.go window.go capture_gdi.go clipboard.go session.go dpi.go open.go(ShellExecute)
│   │   └── fake/               # 테스트용 이벤트 기록 backend
│   ├── transport/
│   │   ├── transport.go        # Runner 인터페이스(IR + run 옵션 → JSONL 이벤트 스트림), 원격 모드 감지(SSH_CONNECTION/SSH_TTY, Windows 세션 검사)
│   │   ├── local/              # in-process: engine.Run + backend.Local; 원격 모드면 bridge 클라이언트로 위임
│   │   └── ssh/                # `ssh <dest> <remote-bin> local ...` spawn, [f] 인라인 텍스트를 stdin으로, JSONL 소비, base64 캡처 → 로컬 파일, exit 3/5 판정, deadline+5s kill
│   ├── bridge/
│   │   ├── proto.go            # 요청(IR + run / request_perms) 디코드, JSONL 응답
│   │   ├── server.go           # --bridge: 단일 run 뮤텍스, 호출자 단절 = 취소, 로그
│   │   ├── client.go           # 소켓 연결, IR 전송, 이벤트 중계
│   │   ├── sock_darwin.go      # unix socket ~/Library/Application Support/gotto-hando/bridge.sock (0600/0700)
│   │   └── pipe_windows.go     # named pipe \\.\pipe\gotto-hando-<username>, 현재 사용자 ACL
│   ├── output/                 # plain/jsonl 라이터(--inline-captures 디코드 포함), exit code 계산
│   ├── capture/                # 픽셀 → PNG/JPEG 인코딩, 스케일링
│   └── perms/                  # --request-perms (darwin: AXIsProcessTrustedWithOptions, CGRequestScreenCaptureAccess; 원격 모드면 브릿지 경유; windows exit 2)
├── docs/                       # 설계 노트(선택; 규범은 assets/에만)
├── testdata/                   # 문법 골든 파일, IR 스냅샷
├── scripts/                    # 크로스 빌드, codesign(자체 서명 인증서 `gotto-hando-dev`로 재서명, 9.1·help-macos), 릴리스
└── .goreleaser.yaml            # darwin/arm64, darwin/amd64, windows/amd64
```

빌드: 모든 타깃이 `CGO_ENABLED=0`. macOS 네이티브 API는 purego dlopen(9.1), Windows는 `x/sys/windows`이므로 `GOOS=darwin|windows GOARCH=arm64|amd64 go build`가 **한 호스트에서** 전부 된다(Linux CI 포함). macOS 호스트가 필요한 단계는 `codesign` 재서명만이며, 이는 릴리스 스크립트에서 선택 단계다(서명 없이도 터미널 사용은 동작, 9.1).

---

## 13. 로드맵

| 마일스톤 | 범위 | 완료 기준 |
|---|---|---|
| **M0** 파서 + IR + 로컬 macOS | `syntax`, `ir`, `engine`, `backend/darwin`(입력·캡처·윈도우·클립보드), `output`, `--help*`(assets embed 출력), `--check/--ir`, `--request-perms`, codesign 스크립트 | `gotto-hando local` 로 help.txt `== COMMANDS ==`의 커맨드 전부 동작(M1 `open`·`exec`, M3 `qwin` 필터, M4 `fmt=jpg`·`cursor` 제외). engine 계약 테스트(cua-batch 테스트 이식) 통과. 3.3의 드리프트 테스트 1·2·3·8 통과 |
| **M1** Windows backend + 프로세스 | `backend/windows`, 세션/DPI/포커스 트릭, 크로스 빌드, `open`·`exec`(5.4, help.txt COMMANDS; 양 플랫폼, `noerr`, 로그인 셸, 코드페이지 변환), `--help-windows` | Windows에서 `local` 동일 동작. Blender 수치 입력 시나리오 재현. `open`/`exec`가 macOS·Windows `local`에서 동작(출력 캡·타임아웃 kill·CP949 출력의 UTF-8 변환 포함) |
| **M2** ssh 래퍼 + 세션 브릿지 | `transport/ssh`(spawn, stdin 시퀀스, JSONL 소비, base64 캡처 → 로컬 파일, exit 3/5 판정, deadline+5s kill), `[f]` 인라인 재작성, `--inline-captures`, `--remote-bin`; `bridge`(unix socket / named pipe, ACL, 단일 run, 취소, 로그), 원격 모드 감지(`SSH_CONNECTION`/`SSH_TTY`, Windows 세션 검사), `--request-perms` 브릿지 경유, `qinfo session=bridge`, `--help-remote` | Mac 에이전트 → `ssh winbox` → Windows 브릿지로 캡처 왕복 < 500ms(ControlMaster 재사용 시); 브릿지 종료 시 즉시 `E_SESSION` exit 4; ssh 단절 시 exit 5 + 원격 held 해제 |
| **M3** 윈도우/앱 편의 | `qwin` 필터, 비-US 레이아웃 키 매핑 점검 | |
| **M4** 캡처 고도화 | ScreenCaptureKit, DXGI, `fmt=jpg`, `cap[cursor]`, 버스트 성능 | |
| **M5** 운영 편의 | 브릿지 설치 헬퍼(LaunchAgent plist / 작업 스케줄러 등록·해제), ssh `ControlMaster` 안내 자동화 | |

---

## 14. Open questions

확정되어 규범(help 텍스트)으로 옮긴 항목(더 이상 open이 아님; 근거는 괄호의 장): space-form 폐지·bracket-form 필수(4.1), `rect=` 값의 `:` 구분자(4.2), `[f]` 경로 trim(4.2), Meta=`m`·Win 별칭 flag 없음(4.3), `%`+`r` 금지·프레임 모디파이어 단일(4.4), 코드 8키에 flag 포함(5.2), `scroll` 틱 수 payload 단일화(5.2), `exec`/`open` 임의 실행 허용(5.4, 10장), `exec` exit ≠ 0 기본 `err` + `noerr` flag(5.4), `exec[shell]` = 로그인 셸·Windows `cmd /C`(5.4), Windows `exec` 출력 코드페이지→UTF-8 변환·U+FFFD 치환(5.4, 9.2), `exec` timeout 상한 60s·IR 필드명·결과 줄 형식(5.4, help.txt/help-remote), `exec`·`open` 기본 delay 제외·조회 커맨드의 명시 `d=`(6.3), 기본 delay 100ms(6.3), 실행 측(브릿지) 데드라인 강제 + 로컬 +5s kill(6.5), `-f`/argv 혼용 금지(7.1), `--request-perms` 브릿지 경유(7.4), 원격 = ssh 래핑·네트워크 리스너 없음·설정 파일 없음(8.3, 8.4, 10장), 세션 브릿지 양 플랫폼 필수·consent gate(8.3, 10장), `[f]`는 에이전트 머신에서 해석·인라인 전송(8.1), macOS purego·전 타깃 `CGO_ENABLED=0`(2.3, 9.1, 12장), `--help` 계열 영어·`--help [topic]` 미채택·플랫폼/원격 도움말 3종 분리·help 텍스트 = SoT(3장).

1. **`p`(primary) 외에 `c`를 primary로 재해석하는 호환 옵션**(`--ctrl-is-primary`)이 필요한가? 현재는 미제공.
2. **base64 인라인 스트림의 크기**: 버스트 캡처(`n=120`)에서 ssh stdout 스트림 크기가 문제면 청킹이나 ssh 압축(`Compression yes`) 권고 추가.
3. **`txt`의 `\n` 처리**: Return 키 입력으로 정의했으나, 일부 앱은 Shift+Enter가 개행이다. `txt[nl=key|char]` 옵션 필요 여부.
4. **줄 단위 soft-fail(비-`exec`)**: `-k` 전역 옵션만 제공. `exec`는 `noerr`로 해결됐지만, 특정 줄만 실패 허용(예: 있을 수도 없을 수도 있는 다이얼로그 닫기)하는 일반 문법(`win[opt]...`?)이 필요한지.
5. **키 이름 별칭 유지 범위**: `meta`=`cmd`=`win`은 확정. `option`=`alt`, `return`=`enter` 등 나머지 별칭을 유지할지.
6. **클립보드 복원**: `paste` 후 이전 클립보드를 복원하는 옵션(`restore` flag). 사람과 데스크톱을 공유하는 경우 유용.
7. **Windows 관리자 권한 앱**: UIPI 차단을 브릿지가 감지해 `E_INPUT` 힌트를 줄 수 있는가(`GetLastError`로는 구분 불가한 경우 많음). 현재는 `qinfo`의 `elevated=` 보고와 `--help-windows` 안내로 대신한다(9.2).
8. **여러 PC 동시 제어**: 한 호출에서 여러 `<dest>`를 섞는 문법은 제공하지 않음(각 호출 = 한 PC). 확정 여부.
9. **브릿지 대기 정책**: 두 번째 호출자가 "대기"하는 현재 규칙 vs 즉시 `E_BUSY` 거부. 에이전트가 병렬로 호출하는 경우의 예측 가능성 관점에서 재검토.
10. **원격 모드 감지의 오탐**: GUI 터미널 안에서 `ssh localhost gotto-hando local`처럼 자기 자신에게 ssh한 경우도 `SSH_CONNECTION`이 있어 브릿지를 요구한다(의도된 동작). 반대로 tmux/screen 등이 `SSH_CONNECTION`을 물려주는 환경에서 GUI 터미널 호출이 원격 모드로 오판될 수 있는지 — 필요하면 `--no-bridge` 강제 옵션 검토.
