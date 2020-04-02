package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"math"
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
	lastError       string
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

func (s *state) SwitchTag(tag db.Tag) {
	err := s.DeleteTagIfEmpty(s.CurrentDBTag.ID)
	checkErr(err)

	s.CurrentDBTag = tag
	s.Refresh()
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

	s.SwitchTag(tag)
}

func (s *state) NewTag(arg string) {
	var tag db.Tag
	var err error

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newTagName, ok := GetTextFromEditor(nil)
		if !ok {
			s.lastError = "editor exited abnormally"
			return
		}
		if len(newTagName) == 0 {
			s.lastError = "empty input"
			return
		}
		tag, err = s.DB.AddTag(string(newTagName))
		checkErr(err)
	} else {
		tag, err = s.DB.AddTag(arg)
		checkErr(err)
	}

	s.lastError = ""
	s.SwitchTag(tag)
}

func (s *state) RenderMain() {
	var tag db.Tag
	var err error

	rowKey := NewIncrementingKey()

	clearScreen()
	fmt.Printf("== %s ==\n", s.CurrentDBTag.Name)

	s.rowShortcuts = make(map[string]db.Row)

	re := regexp.MustCompile(`\[\[(.*?)\]\]`)

	for _, row := range s.CurrentDBRows {
		s.rowShortcuts[rowKey.String()] = row
		fmt.Printf(" %s: ", rowKey)
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
				fmt.Printf("  %s: ", rowKey)
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

	if s.lastError != "" {
		fmt.Printf("\n%s", s.lastError)
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
			s.lastError = "editor exited abnormally"
			return
		}
		if len(newTagName) == 0 {
			s.lastError = "empty input"
			return
		}

		tag, err = s.DB.RenameTag(s.CurrentDBTag.Name, string(newTagName))
		checkErr(err)
	} else {
		tag, err = s.DB.RenameTag(s.CurrentDBTag.Name, arg)
		checkErr(err)
	}

	s.lastError = ""
	s.SwitchTag(tag)
}

func (s *state) SelectTag(arg string) {
	var search string
	var filteredTags []db.Tag

	clearScreen()

	arg = strings.TrimSpace(arg)

	if len(arg) > 0 {
		if arg[0] == '/' {
			search = arg[1:]
			if search == "" {
				s.lastError = "empty search"
				return
			}
		}
	}

	keys := make(map[string]db.Tag)
	key := NewIncrementingKey()

	// exact match attempt
	if search == "" && len(arg) > 0 {
		for _, v := range s.AllDBTags {
			if v.Name == arg {
				s.lastError = ""
				s.SwitchTag(v)
				return
			}
		}
		s.lastError = fmt.Sprintf("tag %s does not exist", arg)
		return
	}

	// fuzzy match attempt
	if len(search) > 0 {
		filteredTags = make([]db.Tag, 0)
		for _, v := range s.AllDBTags {
			if strings.Contains(strings.ToLower(v.Name), strings.ToLower(search)) {
				filteredTags = append(filteredTags, v)
			}
		}
		switch len(filteredTags) {
		case 0:
			s.lastError = fmt.Sprintf("search for \"%s\" returned no tags", search)
			return
		case 1:
			s.lastError = ""
			s.SwitchTag(filteredTags[0])
			return
		default:
			// move on to tag selection menu
		}
	} else {
		// no args were passed; all tags
		filteredTags = s.AllDBTags
	}

	if len(search) > 0 {
		fmt.Printf("== Tags matching \"%s\" ==\n", search)
	} else {
		fmt.Println("== All Tags ==")
	}
	for _, v := range filteredTags {
		fmt.Printf(" %s: %s\n", key.String(), v.Name)
		keys[key.String()] = v
		key.increment()
	}
	fmt.Printf("\n[selection]: ")
	s.scanner.Scan()
	selection := s.scanner.Text()

	if len(selection) == 0 {
		s.lastError = ""
		return
	}

	if tag, ok := keys[selection]; ok {
		s.lastError = ""
		s.SwitchTag(tag)
	} else {
		s.lastError = "invalid input"
	}
}

func GetTextFromEditor(initialText []byte) ([]byte, bool) {
	var text []byte
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
		text, err = ioutil.ReadFile(f.Name())
		os.Remove(f.Name())
		// strip trailing/leading whitespace
		text = []byte(strings.TrimSpace(string(text)))
	}
	return text, true
}

func (s *state) NewRow(arg string) {
	var newRowText []byte
	var ok bool

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newRowText, ok = GetTextFromEditor(nil)
		if !ok {
			s.lastError = "editor exited abnormally"
			return
		}
		if len(newRowText) == 0 {
			s.lastError = "empty input"
			return
		}
	} else {
		newRowText = []byte(arg)
	}

	_, err := s.DB.AddRow(s.CurrentDBTag.ID, string(newRowText), 0)
	checkErr(err)

	s.lastError = ""
	s.Refresh()
}

func (s *state) DeleteRow(arg string) {
	arg = strings.TrimSpace(arg)
	if row, ok := s.rowShortcuts[arg]; ok {
		err := s.DB.DeleteRowByID(row.ID)
		checkErr(err)

		s.lastError = ""
		s.Refresh()
	} else {
		s.lastError = fmt.Sprintf("invalid row: %s", arg)
	}
}

func (s *state) EditRow(arg string) {
	arg = strings.TrimSpace(arg)
	if row, ok := s.rowShortcuts[arg]; ok {
		newRowText, ok := GetTextFromEditor([]byte(row.Text))
		if !ok {
			s.lastError = "editor exited abnormally"
			return
		}
		if len(newRowText) == 0 {
			s.lastError = "empty input"
			return
		}

		err := s.DB.UpdateRowText(row.ID, string(newRowText))
		checkErr(err)

		s.lastError = ""
		s.Refresh()
	}
}

func (s *state) MoveRow(arg string) {
	arg = strings.TrimSpace(arg)
	// need exactly two args
	args := strings.Fields(arg)
	if len(args) != 2 {
		s.lastError = "move <a> <b>"
		return
	}
	if len(s.CurrentDBRows) == 0 {
		s.lastError = "no rows"
		return
	}
	if keyToInt(args[0]) >= len(s.CurrentDBRows) {
		s.lastError = fmt.Sprintf("%s out of range", args[0])
		return
	}
	if keyToInt(args[1]) >= len(s.CurrentDBRows) {
		s.lastError = fmt.Sprintf("%s out of range", args[1])
		return
	}

	if row, ok := s.rowShortcuts[args[0]]; ok {
		err := s.DB.UpdateRowRank(row.ID, keyToInt(args[1]))
		checkErr(err)
		s.lastError = ""
		s.Refresh()
	}
}

func (s *state) printHelp() {
	clearScreen()
	fmt.Println("h: jump to today tag")
	fmt.Println("[0-9]*: jump to shown numbered tag")
	fmt.Println("a [text]: add new row with text [text] or fire up editor if [text] is not present")
	fmt.Println("d <row>: delete row")
	fmt.Println("e <row>: edit row")
	fmt.Println("t: tag menu")
	fmt.Println("t/<text>: search tag names for <text>")
	fmt.Println("t <text>: jump to exact tag <text>")
	fmt.Println("m <row1> <row2>: move row1 to row2")
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
			programState.lastError = ""
			programState.Refresh()
			programState.RenderMain()
			continue
		}

		switch line[0] {
		case 'h':
			programState.lastError = ""
			programState.GoToToday()
		case 'a':
			programState.NewRow(line[1:])
		case 'd':
			programState.DeleteRow(line[1:])
		case 'e':
			programState.EditRow(line[1:])
		case 'm':
			programState.MoveRow(line[1:])
		case 'n':
			programState.NewTag(line[1:])
		case 't':
			programState.SelectTag(line[1:])
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
				programState.lastError = "invalid input"
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
