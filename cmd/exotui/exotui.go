package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/neutralinsomniac/exocortex/db"
)

type state struct {
	db.State
	scanner         *bufio.Scanner
	rowShortcuts    map[string]db.Row
	tagShortcuts    map[db.Tag]int
	tagShortcutsRev map[int]db.Tag
}

type incrementingKey struct {
	key string
}

func (c *incrementingKey) increment() {
	for i := len(c.key) - 1; i >= 0; i-- {
		if c.key[i] < 'z' {
			c.key = c.key[:i] + string(c.key[i]+1) + string(c.key[i+1:])
			return
		}
		// key at cur position is 'z'; reset to 'a'
		c.key = c.key[:i] + "a" + string(c.key[i+1:])
	}
	// we're at the end of the line; add a new power
	c.key = "a" + c.key[:]
}

func NewIncrementingKey() *incrementingKey {
	k := new(incrementingKey)
	k.key = "a"
	return k
}

func (k *incrementingKey) String() string {
	return k.key
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func clearScreen() {
	fmt.Printf("\033[H\033[2J")
}

const ansiReverseVideo = "\033[7m"
const ansiClearParams = "\033[0m"

func (s *state) Refresh() {
	s.State.Refresh()

	// init our tag shortcut map
	s.tagShortcuts = make(map[db.Tag]int)
	s.tagShortcutsRev = make(map[int]db.Tag)
}

func (s *state) GetShortcutForTag(tag db.Tag) int {
	if i, ok := s.tagShortcuts[tag]; ok {
		return i
	}

	maxI := 0
	for _, i := range s.tagShortcuts {
		if i > maxI {
			maxI = i
		}
	}

	key := maxI + 1
	s.tagShortcuts[tag] = key
	s.tagShortcutsRev[key] = tag

	return key
}

func (s *state) GoToToday() {
	t := time.Now()
	tag, err := s.DB.AddTag(t.Format("January 02 2006"))
	checkErr(err)

	s.CurrentDBTag = tag
	s.Refresh()
}

func (s *state) NewTag(arg string) {
	var tag db.Tag
	var err error

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newTagName, ok := GetTextFromEditor(nil)
		if !ok {
			return
		}
		tag, err = s.DB.AddTag(string(newTagName))
		checkErr(err)
	} else {
		tag, err = s.DB.AddTag(arg)
		checkErr(err)
	}

	s.CurrentDBTag = tag
	s.Refresh()
}

func (s *state) RenderMain() {
	var tag db.Tag
	var err error

	rowKey := NewIncrementingKey()

	clearScreen()
	fmt.Printf("== %s ==\n", s.CurrentDBTag.Name)

	s.rowShortcuts = make(map[string]db.Row)

	for _, row := range s.CurrentDBRows {
		s.rowShortcuts[rowKey.String()] = row
		fmt.Printf(" %s: ", rowKey)
		re := regexp.MustCompile(`\[\[(.*?)\]\]`)
		for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
			// leading text
			fmt.Printf("%s", row.Text[:tagIndex[0]])
			// tag
			tag, err = s.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
			checkErr(err)
			fmt.Printf("%s%s(%d)%s", ansiReverseVideo, tag.Name, s.GetShortcutForTag(tag), ansiClearParams)
			row.Text = row.Text[tagIndex[1]:]
		}
		fmt.Printf("%s\n", row.Text)
		rowKey.increment()
	}

	if len(s.CurrentDBRefs) > 0 {
		fmt.Println("\nReferences")
		for _, tag := range s.SortedRefTagsKeys {
			fmt.Printf("\n %s%s(%d)%s\n", ansiReverseVideo, tag.Name, s.GetShortcutForTag(tag), ansiClearParams)
			for _, row := range s.CurrentDBRefs[tag] {
				s.rowShortcuts[rowKey.String()] = row
				fmt.Printf(" %s: ", rowKey)
				re := regexp.MustCompile(`\[\[(.*?)\]\]`)
				for tagIndex := re.FindStringIndex(row.Text); tagIndex != nil; tagIndex = re.FindStringIndex(row.Text) {
					// leading text
					fmt.Printf("%s", row.Text[:tagIndex[0]])
					// tag
					tag, err = s.DB.GetTagByName(row.Text[tagIndex[0]+2 : tagIndex[1]-2])
					checkErr(err)
					fmt.Printf("%s%s(%d)%s", ansiReverseVideo, tag.Name, s.GetShortcutForTag(tag), ansiClearParams)
					row.Text = row.Text[tagIndex[1]:]
				}
				fmt.Printf("%s\n", row.Text)
				rowKey.increment()
			}
		}
	}

	fmt.Printf("\n=> ")
}

func (s *state) RenameTag(arg string) {
	var tag db.Tag
	var err error

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newTagName, ok := GetTextFromEditor([]byte(s.CurrentDBTag.Name))
		if !ok {
			return
		}
		if len(newTagName) == 0 {
			return
		}

		tag, err = s.DB.RenameTag(s.CurrentDBTag.Name, string(newTagName))
		checkErr(err)
	} else {
		tag, err = s.DB.RenameTag(s.CurrentDBTag.Name, arg)
		checkErr(err)
	}

	s.CurrentDBTag = tag
	s.Refresh()
}

func (s *state) SelectTagMenu() {
	fmt.Println("== Tags ==")
	for i, v := range s.AllDBTags {
		fmt.Printf(" %d: %s\n", i+1, v.Name)
	}
	fmt.Printf("\n[num or /search]: ")
	s.scanner.Scan()
	selection, err := strconv.Atoi(s.scanner.Text())
	if err != nil {
		return
	}

	if selection > len(s.AllDBTags) || selection <= 0 {
		return
	}

	s.CurrentDBTag = s.AllDBTags[selection-1]
	s.Refresh()
}

func GetTextFromEditor(initialText []byte) ([]byte, bool) {
	var newRowText []byte
	var err error
	var f *os.File

	if f, err = ioutil.TempFile("", "exo"); err == nil {
		f.Write(initialText)
		cmd := exec.Command("vi", f.Name())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return nil, false
		}
		newRowText, err = ioutil.ReadFile(f.Name())
		os.Remove(f.Name())
		// strip trailing/leading whitespace
		newRowText = []byte(strings.TrimSpace(string(newRowText)))
	}
	return newRowText, true
}

func (s *state) NewRow(arg string) {
	var newRowText []byte
	var ok bool

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newRowText, ok = GetTextFromEditor(nil)
		if !ok {
			return
		}
		if len(newRowText) == 0 {
			return
		}
	} else {
		newRowText = []byte(arg)
	}
	s.DB.AddRow(s.CurrentDBTag.ID, string(newRowText), 0, 0)
	s.Refresh()
}

func (s *state) DeleteRow(arg string) {
	arg = strings.TrimSpace(arg)
	if row, ok := s.rowShortcuts[arg]; ok {
		err := s.DB.DeleteRowByID(row.ID)
		checkErr(err)

		err = s.DeleteTagIfEmpty(row.TagID)
		checkErr(err)

		err = s.DeleteTagIfEmpty(s.CurrentDBTag.ID)
		checkErr(err)

		// if current tag is gone, switch
		if _, err := s.DB.GetTagByID(s.CurrentDBTag.ID); err != nil {
			s.GoToToday()
		}
		s.Refresh()
	}
}

func (s *state) EditRow(arg string) {
	arg = strings.TrimSpace(arg)
	if row, ok := s.rowShortcuts[arg]; ok {
		newRowText, ok := GetTextFromEditor([]byte(row.Text))
		if !ok {
			return
		}
		if len(newRowText) == 0 {
			return
		}
		err := s.DB.UpdateRowText(row.ID, string(newRowText))
		checkErr(err)
		s.Refresh()
	}
}

func (s *state) printHelp() {
	clearScreen()
	fmt.Println("h: jump to today tag")
	fmt.Println("[0-9]*: jump to shown numbered tag")
	fmt.Println("a [text]: add new row with text [text] or fire up editor if [text] is not present")
	fmt.Println("d <letter>: delete row designated by <letter>")
	fmt.Println("e <letter>: edit row designated by <letter>")
	fmt.Println("t: tag menu")
	fmt.Println("n [text]: new tag with text <text>")
	fmt.Println("r [text]: rename current tag with text <text>")
	fmt.Println("?: print help")
	fmt.Println("press [enter] to continue...")

	s.scanner.Scan()
}

func main() {
	var err error
	var programState state

	programState.DB = &db.ExoDB{}

	err = programState.DB.Open("./exocortex.db")
	checkErr(err)

	err = programState.DB.LoadSchema()
	checkErr(err)

	programState.GoToToday()
	programState.Refresh()

	scanner := bufio.NewScanner(os.Stdin)
	programState.scanner = scanner
	programState.RenderMain()

	for scanner.Scan() {
		line := scanner.Text()

		if len(line) == 0 {
			programState.RenderMain()
			continue
		}

		switch line[0] {
		case 'h':
			programState.GoToToday()
		case 'a':
			programState.NewRow(line[1:])
		case 'd':
			programState.DeleteRow(line[1:])
		case 'e':
			programState.EditRow(line[1:])
		case 'n':
			programState.NewTag(line[1:])
		case 't':
			programState.SelectTagMenu()
		case 'r':
			programState.RenameTag(line[1:])
		case '?':
			programState.printHelp()
		case 'q':
			goto End
		default:
			// try to parse as int
			i, err := strconv.Atoi(line)
			if err != nil {
				break
			}
			if tag, ok := programState.tagShortcutsRev[i]; ok {
				programState.CurrentDBTag = tag
				programState.Refresh()
			}
		}
		programState.RenderMain()
	}
End:
	fmt.Println("bye!")
}
