package familiars

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fake(answers map[string]string, failure error) *Client {
	return &Client{Path: "familiard", run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if failure != nil {
			return nil, failure
		}
		return []byte(answers[args[0]]), nil
	}}
}

func TestListReadsTheIds(t *testing.T) {
	ids, err := fake(map[string]string{"pets": `[{"id":"a","name":"A"},{"id":"b","name":"B"}]`}, nil).List(context.Background())
	if err != nil || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("got %v, %v", ids, err)
	}
}

func TestLoad(t *testing.T) {
	good := `{"id":"a","name":"A\u001b[31m!","rows":2,"columns":3,"clips":{"idle":{"fps":2,"frames":["x\ny"]}}}`
	tests := []struct {
		name    string
		answer  string
		failure error
		wantErr error
		anyErr  bool
	}{
		{name: "good", answer: good},
		{name: "too old", failure: errors.New("frames a: exit 1: familiard: unknown command frames"), wantErr: ErrOutdated},
		{name: "failing", failure: errors.New("no pet 'a'"), anyErr: true},
		{name: "not json", answer: "hello", anyErr: true},
		{name: "wrong size", answer: `{"id":"a","rows":9,"columns":3,"clips":{"idle":{"fps":2,"frames":["x"]}}}`, anyErr: true},
		{name: "no clips", answer: `{"id":"a","rows":2,"columns":3,"clips":{}}`, anyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack, err := fake(map[string]string{"frames": tt.answer}, tt.failure).Load(context.Background(), "a", 2)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got %v", err)
				}
			case tt.anyErr:
				if err == nil {
					t.Fatal("wanted an error")
				}
			default:
				if err != nil || pack.Columns != 3 {
					t.Fatalf("got %v, %v", pack, err)
				}
				if pack.Name != "A[31m!" {
					t.Errorf("a name must not carry terminal commands: %q", pack.Name)
				}
			}
		})
	}
}

func TestFrameMovesWithTimeAndOffset(t *testing.T) {
	p := &Pack{Clips: map[string]Clip{"idle": {FPS: 2, Frames: []string{"a", "b", "c"}}}}
	at := func(ms int64, offset int) string { return p.Frame("idle", time.UnixMilli(ms), offset) }
	if at(0, 0) != "a" || at(500, 0) != "b" || at(1000, 0) != "c" || at(1500, 0) != "a" {
		t.Errorf("frames do not follow the clip's speed")
	}
	if at(0, 1) != "b" {
		t.Errorf("an offset should shift the frame")
	}
	if p.Frame("working", time.UnixMilli(0), 0) != "" {
		t.Errorf("a missing clip draws nothing")
	}
}

func TestAssignerKeepsPetsApartWhileItCan(t *testing.T) {
	got := NewAssigner().Sync([]string{"s1", "s2", "s3"}, []string{"a", "b", "c"})
	seen := map[string]bool{}
	for _, pet := range got {
		seen[pet] = true
	}
	if len(got) != 3 || len(seen) != 3 {
		t.Fatalf("every session should get its own pet: %v", got)
	}
}

func TestAssignerRepeatsOnlyWhenPetsRunOut(t *testing.T) {
	got := NewAssigner().Sync([]string{"s1", "s2", "s3"}, []string{"a"})
	if len(got) != 3 || got["s1"] != "a" || got["s3"] != "a" {
		t.Fatalf("got %v", got)
	}
}

func TestAssignerKeepsAChoiceWhenOthersComeAndGo(t *testing.T) {
	pets := []string{"a", "b", "c", "d", "e", "f"}
	assigner := NewAssigner()
	first := assigner.Sync([]string{"z"}, pets)["z"]
	if got := assigner.Sync([]string{"a0", "z", "b0"}, pets)["z"]; got != first {
		t.Errorf("a session changed pet when others arrived: %s then %s", first, got)
	}
	if got := assigner.Sync([]string{"z"}, pets); len(got) != 1 || got["z"] != first {
		t.Errorf("a session changed pet when others left: %v", got)
	}
}

func TestAssignerIsTheSameAcrossRestarts(t *testing.T) {
	pets := []string{"a", "b", "c", "d"}
	one := NewAssigner().Sync([]string{"session"}, pets)["session"]
	two := NewAssigner().Sync([]string{"session"}, []string{"d", "c", "b", "a"})["session"]
	if one != two {
		t.Errorf("%s then %s", one, two)
	}
}

func TestAssignerDropsAPetThatLeftTheGallery(t *testing.T) {
	assigner := NewAssigner()
	assigner.Sync([]string{"s"}, []string{"a"})
	if got := assigner.Sync([]string{"s"}, []string{"b"})["s"]; got != "b" {
		t.Errorf("got %s", got)
	}
	if got := assigner.Sync([]string{"s"}, nil); len(got) != 0 {
		t.Errorf("with no pets nothing is chosen: %v", got)
	}
}

func TestFindPrefersWhatIsOnThePath(t *testing.T) {
	own := t.TempDir()
	if err := os.WriteFile(filepath.Join(own, ProgramName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	onPath := t.TempDir()
	t.Setenv("PATH", onPath)
	if got := Find(own); got == nil || got.Path != filepath.Join(own, ProgramName()) {
		t.Fatalf("should find the one claudeops installed: %+v", got)
	}
	if got := Find(t.TempDir()); got != nil {
		t.Fatalf("nothing installed, nothing found: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(onPath, ProgramName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Find(own); got == nil || got.Path != filepath.Join(onPath, ProgramName()) {
		t.Fatalf("the user's own familiard must win: %+v", got)
	}
}
