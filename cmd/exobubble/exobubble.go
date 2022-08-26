package main

import (
	"fmt"
	"math"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
	cursor      int
	allTagNames map[string]bool
	tagStack    []string // tag.Name
	lastError   string
}

type incrementingKey struct {
	key []byte
}

func (c *incrementingKey) Increment() {
	for i := len(c.key) - 1; i >= 0; i-- {
		if c.key[i] < 'z' {
			c.key[i]++
			return
		}
		// key at cur position is 'z'; reset to 'a'
		c.key[i] = 'a'
	}
	// we're at the end of the line; add a new power
	c.key = append([]byte{'a'}, c.key...)
}

func NewIncrementingKey(init string) *incrementingKey {
	k := new(incrementingKey)
	if init == "" {
		k.key = []byte{'a'}
	} else {
		k.key = []byte(init)
	}
	return k
}

func (k *incrementingKey) String() string {
	return string(k.key)
}

// return the 0-indexed rank of the given key
func keyToInt(key string) int {
	num := 0
	for i := len(key) - 1; i >= 0; i-- {
		pow := math.Pow(26, float64(len(key)-1-i))
		num += int(key[i]-'a'+1) * int(pow)
	}

	return num - 1
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (s *state) Refresh() {
	s.State.Refresh()

	// cache tag names for calendar
	s.allTagNames = make(map[string]bool)
	for _, tag := range s.AllDBTags {
		s.allTagNames[tag.Name] = true
	}

}

func (s *state) SwitchTag(tag db.Tag) {
	if tag.ID != s.CurrentDBTag.ID && s.CurrentDBTag.ID != 0 {
		err := s.DeleteTagIfEmpty(s.CurrentDBTag.ID)
		checkErr(err)
		// if we're switching to a new tag, push it onto our tag stack
		s.tagStack = append(s.tagStack, s.CurrentDBTag.Name)
	}

	s.CurrentDBTag = tag
	s.Refresh()
}

func (s *state) PopTag() {
	if len(s.tagStack) == 0 {
		s.lastError = "tag stack empty"
		return
	}

	l := len(s.tagStack)
	tagName := s.tagStack[l-1]
	s.tagStack = s.tagStack[:l-1]

	tag, err := s.DB.AddTag(tagName)
	checkErr(err)

	// have to do the manual tag switch dance since SwitchTag() will push onto the tag stack
	if tag.ID != s.CurrentDBTag.ID {
		err := s.DeleteTagIfEmpty(s.CurrentDBTag.ID)
		checkErr(err)
	}

	s.CurrentDBTag = tag
	s.Refresh()
	s.lastError = ""

	return
}

func (s *state) GoToToday() {
	t := time.Now()
	s.GoToDate(t)
}

func (s *state) GoToDate(t time.Time) {
	tag, err := s.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	s.lastError = ""
	s.SwitchTag(tag)
}

func (s state) Init() tea.Cmd {
	return nil
}

func (s state) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Is it a key press?
	case tea.KeyMsg:

		// Cool, what was the actual key pressed?
		switch msg.String() {

		// These keys should exit the program.
		case "ctrl+c", "q":
			return s, tea.Quit

		// The "up" and "k" keys move the cursor up
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}

		// The "down" and "j" keys move the cursor down
		case "down", "j":
			if s.cursor < len(s.AllDBTags)-1 {
				s.cursor++
			}

			// The "enter" key and the spacebar (a literal space) toggle
			// the selected state for the item that the cursor is pointing at.
		}
	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return s, nil
}

func (s state) View() string {
	// The header
	str := "What should we buy at the market?\n\n"

	// The footer
	str += "\nPress q to quit.\n"

	// Send the UI for rendering
	return str
}

func initialState() state {
	var programState state
	var err error

	programState.DB = &db.ExoDB{}

	err = programState.DB.Open("./exocortex.db")
	checkErr(err)

	err = programState.DB.LoadSchema()
	checkErr(err)

	programState.GoToToday()
	programState.Refresh()

	return programState
}

func main() {
	p := tea.NewProgram(initialState())
	if err := p.Start(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
