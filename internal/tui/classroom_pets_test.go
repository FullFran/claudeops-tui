package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/familiars"
	"github.com/fullfran/claudeops-tui/internal/live"
)

func testPack(name string) *familiars.Pack {
	art := strings.TrimSuffix(strings.Repeat("▀▀▀▀▀▀▀▀▀▀\n", 3), "\n")
	clip := func() familiars.Clip { return familiars.Clip{FPS: 1, Frames: []string{art}} }
	return &familiars.Pack{ID: name, Name: name, Rows: 3, Columns: 10, Clips: map[string]familiars.Clip{
		"idle": clip(), "working": clip(), "waiting": clip(),
	}}
}

func classroomModel(client *familiars.Client, sessions ...string) Model {
	m := Model{activeTab: TabClassroom, width: 200}.WithFamiliars(client)
	for _, id := range sessions {
		m.LiveSessions = append(m.LiveSessions, live.Session{SessionID: id, ProjectName: "proj-" + id, State: live.StateWorking})
	}
	return m
}

func TestWithoutFamiliardTheTabKeepsItsMonkeys(t *testing.T) {
	m := classroomModel(nil, "s1")
	got := renderClassroomTab(m)
	if !strings.Contains(got, "ᴥ") {
		t.Errorf("monkeys should stay; got:\n%s", got)
	}
	if !strings.Contains(got, "claudeops familiard install") {
		t.Errorf("the tab should say how to get familiars; got:\n%s", got)
	}
	if cmds := m.classroom.next(true, m.LiveSessions); cmds != nil {
		t.Errorf("no familiard means nothing to run, got %d commands", len(cmds))
	}
}

func TestAModelThatNeverAskedForFamiliarsSaysNothing(t *testing.T) {
	m := Model{}
	m.LiveSessions = []live.Session{{SessionID: "s", ProjectName: "p", State: live.StateWorking}}
	got := renderClassroomTab(m)
	if strings.Contains(got, "familiard") || !strings.Contains(got, "ᴥ") {
		t.Errorf("got:\n%s", got)
	}
	if cmds := m.classroom.next(true, m.LiveSessions); cmds != nil {
		t.Errorf("got %d commands", len(cmds))
	}
}

func TestNothingRunsWhileTheTabIsClosed(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1")
	if cmds := m.classroom.next(false, m.LiveSessions); cmds != nil {
		t.Errorf("a closed tab must not call familiard, got %d commands", len(cmds))
	}
}

func TestOpeningTheTabAsksOnceForTheGalleryThenForEachPetOnce(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1", "s2")
	c := m.classroom
	if got := len(c.next(true, m.LiveSessions)); got != 1 {
		t.Fatalf("first ask for the gallery, got %d commands", got)
	}
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Fatalf("and only once, got %d commands", got)
	}
	c.listed(familiarsListedMsg{pets: []string{"a", "b"}})
	if got := len(c.next(true, m.LiveSessions)); got != 2 {
		t.Fatalf("then one load per pet in use, got %d commands", got)
	}
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Fatalf("and only once each, got %d commands", got)
	}
}

func TestEachSessionGetsItsFamiliarInPlaceOfTheMonkey(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1", "s2")
	c := m.classroom
	c.next(true, m.LiveSessions)
	c.listed(familiarsListedMsg{pets: []string{"elaina", "kurisu"}})
	c.next(true, m.LiveSessions)
	c.loaded(familiarLoadedMsg{pet: "elaina", pack: testPack("elaina")})
	c.loaded(familiarLoadedMsg{pet: "kurisu", pack: testPack("kurisu")})

	got := renderClassroomTab(m)
	for _, want := range []string{"elaina", "kurisu", "▀", "proj-s1", "proj-s2", "working"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ᴥ") {
		t.Errorf("no monkey should be left; got:\n%s", got)
	}
}

func TestStillALoadingPetLeavesAMonkeyAtItsDesk(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1")
	c := m.classroom
	c.next(true, m.LiveSessions)
	c.listed(familiarsListedMsg{pets: []string{"a"}})
	c.next(true, m.LiveSessions)
	if got := renderClassroomTab(m); !strings.Contains(got, "ᴥ") {
		t.Errorf("got:\n%s", got)
	}
}

func TestAPetThatFailsKeepsItsMonkeyAndIsNotAskedAgain(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1")
	c := m.classroom
	c.next(true, m.LiveSessions)
	c.listed(familiarsListedMsg{pets: []string{"a"}})
	c.next(true, m.LiveSessions)
	c.loaded(familiarLoadedMsg{pet: "a", err: errors.New("broken sheet")})
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Errorf("got %d commands", got)
	}
	if got := renderClassroomTab(m); !strings.Contains(got, "ᴥ") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHints(t *testing.T) {
	c := classroomModel(familiars.NewClient("familiard")).classroom
	if c.hint() != "" {
		t.Errorf("nothing to say before an answer: %q", c.hint())
	}
	c.listed(familiarsListedMsg{})
	if c.hint() != emptyHint {
		t.Errorf("got %q", c.hint())
	}
	c.listed(familiarsListedMsg{pets: []string{"a"}})
	if c.hint() != "" {
		t.Errorf("got %q", c.hint())
	}
	c.loaded(familiarLoadedMsg{pet: "a", err: familiars.ErrOutdated})
	if c.hint() != updateHint {
		t.Errorf("got %q", c.hint())
	}
}

func TestAnOldFamiliardIsNotAskedForMorePets(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1", "s2")
	c := m.classroom
	c.next(true, m.LiveSessions)
	c.listed(familiarsListedMsg{pets: []string{"a", "b"}})
	c.next(true, m.LiveSessions)
	c.loaded(familiarLoadedMsg{pet: "a", err: familiars.ErrOutdated})
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Errorf("got %d commands", got)
	}
}

func TestAnimationRunsOnlyWithPetsOnAnOpenTabAndNeverTwice(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1")
	c := m.classroom
	c.asked, c.answered, c.pets = true, true, []string{"a"}
	c.loading["a"] = true
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Errorf("nothing to animate yet, got %d commands", got)
	}
	c.loaded(familiarLoadedMsg{pet: "a", pack: testPack("a")})
	if got := len(c.next(true, m.LiveSessions)); got != 1 || !c.ticking {
		t.Errorf("one tick, got %d commands", got)
	}
	if got := len(c.next(true, m.LiveSessions)); got != 0 {
		t.Errorf("never two at once, got %d commands", got)
	}
	c.ticking = false
	if got := len(c.next(false, m.LiveSessions)); got != 0 || c.ticking {
		t.Errorf("a closed tab stops animating, got %d commands", got)
	}
}

func TestUpdateCarriesTheAnswersIntoTheTab(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1")
	var model tea.Model = m
	model, _ = model.Update(familiarsListedMsg{pets: []string{"a"}})
	model, _ = model.Update(familiarLoadedMsg{pet: "a", pack: testPack("a")})
	got := model.(Model)
	if got.classroom.packFor("s1") == nil {
		t.Fatal("the session should have its pet")
	}
	got.classroom.ticking = true
	model, _ = model.Update(classroomFrameMsg{})
	if !model.(Model).classroom.ticking {
		t.Error("the next frame should have been scheduled")
	}
}

func TestDesksFlowByTheirRealWidth(t *testing.T) {
	m := classroomModel(familiars.NewClient("familiard"), "s1", "s2")
	c := m.classroom
	c.next(true, m.LiveSessions)
	c.listed(familiarsListedMsg{pets: []string{"a", "b"}})
	c.next(true, m.LiveSessions)
	c.loaded(familiarLoadedMsg{pet: "a", pack: testPack("a")})
	c.loaded(familiarLoadedMsg{pet: "b", pack: testPack("b")})
	desk := renderFamiliarDesk(m.LiveSessions[0], testPack("a"), time.UnixMilli(0))
	width := lipgloss.Width(desk)
	if width != 10+4 {
		t.Errorf("desk width = %d", width)
	}
	m.width = width
	narrow := lipgloss.Width(renderClassroomTab(m))
	m.width = width*2 + 2
	wide := renderClassroomTab(m)
	if narrow > 70 { // the header line is the widest thing left
		t.Errorf("narrow = %d", narrow)
	}
	if !strings.Contains(wide, "proj-s1") || !strings.Contains(wide, "proj-s2") {
		t.Errorf("got:\n%s", wide)
	}
	sameLine := false
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, "proj-s1") && strings.Contains(line, "proj-s2") {
			sameLine = true
		}
	}
	if !sameLine {
		t.Errorf("two desks should share a row when they fit:\n%s", wide)
	}
}

func TestClipForFollowsTheState(t *testing.T) {
	for st, want := range map[live.State]string{live.StateWorking: "working", live.StateWaiting: "waiting", live.StateIdle: "idle"} {
		if got := clipFor(st); got != want {
			t.Errorf("%v: got %s", st, got)
		}
	}
}
