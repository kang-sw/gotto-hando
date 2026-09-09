package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

func TestRClipImageAndTextRuntime(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "shot.PNG")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewRGBA(image.Rect(0, 0, 2, 3))
	im.SetRGBA(1, 2, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	be := &dryrun.Backend{}
	sum := engine.Run(context.Background(), be, parse(t, "rclip[]"+pngPath, "rclip[]"+textPath), engine.RunOptions{})
	if sum.Exit != 0 || len(sum.Results) != 2 {
		t.Fatalf("summary = %+v", sum)
	}
	if sum.Results[0].Detail == "" || !strings.Contains(sum.Results[0].Detail, "type=image") || !strings.Contains(sum.Results[0].Detail, "2x3") {
		t.Fatalf("image detail = %q", sum.Results[0].Detail)
	}
	if !strings.Contains(sum.Results[1].Detail, "type=text bytes=5") {
		t.Fatalf("text detail = %q", sum.Results[1].Detail)
	}
	if len(be.Calls) < 3 || !strings.HasPrefix(be.Calls[1], "ClipboardSetImage 2x3") || be.Calls[2] != `ClipboardSet "hello"` {
		t.Fatalf("calls = %v", be.Calls)
	}
}

func TestRClipInvalidTextIsClipboardFailureAndKeepGoing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	be := &dryrun.Backend{}
	sum := engine.Run(context.Background(), be, parse(t, "rclip[]"+path, "k[]a"), engine.RunOptions{KeepGoing: true})
	if len(sum.Results) != 2 || sum.Results[0].ErrCode != output.EClipboard || sum.Results[1].Status != "ok" {
		t.Fatalf("results = %+v", sum.Results)
	}
}

func TestRClipClipboardFailureMapsAndControlsContinuation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, keepGoing := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keepGoing), func(t *testing.T) {
			be := &dryrun.Backend{FailOn: func(call string) error {
				if strings.HasPrefix(call, "ClipboardSet ") {
					return errors.New("clipboard failed")
				}
				return nil
			}}
			sum := engine.Run(context.Background(), be, parse(t, "rclip[txt]"+path, "k[]a"), engine.RunOptions{KeepGoing: keepGoing})
			if sum.Results[0].ErrCode != output.EClipboard {
				t.Fatalf("first result = %+v", sum.Results[0])
			}
			want := "skip"
			if keepGoing {
				want = "ok"
			}
			if sum.Results[1].Status != want {
				t.Fatalf("second result = %+v, want %s", sum.Results[1], want)
			}
		})
	}
}

func TestRClipLineDelayOverrideAndRenderedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	globalSeq := parse(t, "rclip[]"+path)
	globalSeq.Defaults.DelayMS = 25
	start := time.Now()
	engine.Run(context.Background(), &dryrun.Backend{}, globalSeq, engine.RunOptions{})
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("global delay ignored: %s", elapsed)
	}
	seq := parse(t, "rclip[d=1ms]"+path)
	seq.Defaults.DelayMS = 25
	start = time.Now()
	sum := engine.Run(context.Background(), &dryrun.Backend{}, seq, engine.RunOptions{})
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("line d= override ignored: %s", elapsed)
	}
	var plain, jsonl bytes.Buffer
	if err := output.WriteResult(&plain, false, false, sum.Results[0]); err != nil {
		t.Fatal(err)
	}
	if err := output.WriteResult(&jsonl, true, false, sum.Results[0]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.String(), "type=text bytes=5") || !strings.Contains(jsonl.String(), `"type":"text"`) || !strings.Contains(jsonl.String(), `"bytes":5`) {
		t.Fatalf("plain=%q json=%q", plain.String(), jsonl.String())
	}
}
