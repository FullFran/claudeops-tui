package tui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/familiars"
	"github.com/fullfran/claudeops-tui/internal/live"
)

const (
	// familiarRows is the smallest size at which a pet still looks like itself.
	familiarRows = 12
	// classroomFrameEvery is quick enough for the fastest clip, and only runs on this tab.
	classroomFrameEvery = 125 * time.Millisecond

	installHint = "Turn these monkeys into familiars: claudeops familiard install"
	emptyHint   = "Your familiard gallery is empty. Add a pet with: familiard add <folder>"
	updateHint  = "Your familiard is too old to come to class. Update it to see your pets here."
)

// classroomPets is what the Classroom knows about familiard: the program, if there is one, and
// the pets it has drawn so far. Nothing here runs until the tab is open.
type classroomPets struct {
	client   *familiars.Client
	assigner *familiars.Assigner
	pets     []string
	asked    bool
	answered bool
	outdated bool
	chosen   map[string]string
	packs    map[string]*familiars.Pack
	loading  map[string]bool
	ticking  bool
}

type familiarsListedMsg struct {
	pets []string
	err  error
}

type familiarLoadedMsg struct {
	pet  string
	pack *familiars.Pack
	err  error
}

type classroomFrameMsg struct{}

// WithFamiliars lets the Classroom use familiard. A nil client means it is not installed, and
// the tab mentions how to get it.
func (m Model) WithFamiliars(client *familiars.Client) Model {
	m.classroom = &classroomPets{
		client:   client,
		assigner: familiars.NewAssigner(),
		packs:    map[string]*familiars.Pack{},
		loading:  map[string]bool{},
	}
	return m
}

// hint is the one line the tab has to say about familiard, if any.
func (c *classroomPets) hint() string {
	switch {
	case c == nil:
		return ""
	case c.client == nil:
		return installHint
	case c.outdated:
		return updateHint
	case c.answered && len(c.pets) == 0:
		return emptyHint
	}
	return ""
}

// packFor returns the drawn pet of a session, or nil while it has none: a monkey takes its desk.
func (c *classroomPets) packFor(session string) *familiars.Pack {
	if c == nil {
		return nil
	}
	return c.packs[c.chosen[session]]
}

func (c *classroomPets) listed(msg familiarsListedMsg) {
	// A gallery that could not be read is not an empty one: the monkeys stay, with nothing said.
	if msg.err == nil {
		c.answered = true
		c.pets = msg.pets
	}
}

func (c *classroomPets) loaded(msg familiarLoadedMsg) {
	// A pet that cannot be drawn stays marked as loading, so nobody keeps asking for it.
	if errors.Is(msg.err, familiars.ErrOutdated) {
		c.outdated = true
	}
	if msg.err == nil {
		c.packs[msg.pet] = msg.pack
		delete(c.loading, msg.pet)
	}
}

// next returns what should happen now that the model changed: asking for the gallery and for any
// pet not drawn yet, and the next frame while there is anything to animate.
func (c *classroomPets) next(open bool, sessions []live.Session) []tea.Cmd {
	if c == nil || c.client == nil || !open {
		return nil
	}
	var cmds []tea.Cmd
	if !c.asked {
		c.asked = true
		client := c.client
		cmds = append(cmds, func() tea.Msg {
			pets, err := client.List(context.Background())
			return familiarsListedMsg{pets: pets, err: err}
		})
	}
	if c.answered && !c.outdated {
		ids := make([]string, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.SessionID)
		}
		c.chosen = c.assigner.Sync(ids, c.pets)
		for _, pet := range c.chosen {
			if c.packs[pet] == nil && !c.loading[pet] {
				c.loading[pet] = true
				cmds = append(cmds, loadFamiliar(c.client, pet))
			}
		}
	}
	if len(c.packs) > 0 && len(sessions) > 0 && !c.ticking {
		c.ticking = true
		cmds = append(cmds, tea.Tick(classroomFrameEvery, func(time.Time) tea.Msg { return classroomFrameMsg{} }))
	}
	return cmds
}

func loadFamiliar(client *familiars.Client, pet string) tea.Cmd {
	return func() tea.Msg {
		pack, err := client.Load(context.Background(), pet, familiarRows)
		return familiarLoadedMsg{pet: pet, pack: pack, err: err}
	}
}
