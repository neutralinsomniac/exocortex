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
	snarfedRows     []db.Row
	allTagNames     map[string]bool
	lastError       string
}

type incrementingKey struct {
	key string
}

func (c *incrementingKey) Increment() {
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

func NewIncrementingKey(init string) *incrementingKey {
	k := new(incrementingKey)
	if init == "" {
		k.key = "a"
	} else {
		k.key = init
	}
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
const ansiBoldText = "\033[1m"
const ansiClearParams = "\033[0m"

func (s *state) Refresh() {
	s.State.Refresh()

	// init our tag shortcut map
	s.tagShortcuts = make(map[db.Tag]int)
	s.tagShortcutsRev = make(map[int]db.Tag)

	// cache tag names for calendar
	s.allTagNames = make(map[string]bool)
	for _, tag := range s.AllDBTags {
		s.allTagNames[tag.Name] = true
	}

}

func (s *state) SwitchTag(tag db.Tag) {
	if tag != s.CurrentDBTag {
		err := s.DeleteTagIfEmpty(s.CurrentDBTag.ID)
		checkErr(err)
	}

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
	s.GoToDate(t)
}

func (s *state) GoToDate(t time.Time) {
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

	rowKey := NewIncrementingKey("")

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
		rowKey.Increment()
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
				rowKey.Increment()
			}
		}
	}

	if s.lastError != "" {
		fmt.Printf("\n%s", s.lastError)
	}

	t := time.Now()
	fmt.Printf("\n[%s] => ", t.Format("15:04:05"))
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
	key := NewIncrementingKey("")

	// jump to tag
	if search == "" && len(arg) > 0 {
		tag, err := s.DB.AddTag(arg)
		checkErr(err)

		s.SwitchTag(tag)

		s.lastError = ""
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
		key.Increment()
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

	if f, err := ioutil.TempFile("", "exo"); err == nil {
		defer os.Remove(f.Name())
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
		// strip trailing/leading whitespace
		text = []byte(strings.TrimSpace(string(text)))
	}
	return text, true
}

func (s *state) printMonthCalendar(t time.Time) {
	today := time.Now()
	year, month, _ := t.Date()

	clearScreen()
	fmt.Printf("  == %s %d ==\n", month.String()[:3], year)

	fmt.Println("S  M  T  W  H  F  S")
	// first, walk the days of the week until we find the start day
	// this aligns us with the proper day of the week before we start
	// printing
	startOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, t.Location())
	for weekdayIter := time.Weekday(0); weekdayIter != startOfMonth.Weekday(); weekdayIter++ {
		fmt.Printf("   ")
	}
	// now we've printed enough space, so we can start blindly walking our days
	var d time.Time
	for d = startOfMonth; d.Month() == t.Month(); d = d.AddDate(0, 0, 1) {
		var isToday, tagExists bool
		if d.Day() == today.Day() && d.Month() == today.Month() && d.Year() == today.Year() {
			isToday = true
		}
		tagStr := d.Format("January 02 2006")
		if _, ok := s.allTagNames[tagStr]; ok {
			tagExists = true
		}
		if isToday {
			fmt.Printf(ansiReverseVideo)
		} else if tagExists {
			fmt.Printf(ansiBoldText)
		}
		dayStr := d.Format("2")
		fmt.Printf("%s", dayStr)
		if isToday || tagExists {
			fmt.Printf(ansiClearParams)
		}
		// and manually pad out
		// can't use format strings because the padding gets included in the video reverse effect
		for i := 0; i < 3-len(dayStr); i++ {
			fmt.Printf(" ")
		}
		if d.Weekday() == time.Saturday {
			fmt.Println("")
		}
	}
	// print a newline only if we haven't already
	if d.Weekday() != time.Sunday {
		fmt.Println("")
	}
}

func (s *state) PickDateInteractive() {
	var currentDate time.Time
	curTagDate, err := time.Parse("January 02 2006", s.CurrentDBTag.Name)
	if err != nil {
		currentDate = time.Now()
	} else {
		currentDate = curTagDate
	}

	s.printMonthCalendar(currentDate)
	fmt.Println("\nenter '?' for help")
	fmt.Printf("\n=> ")

	for s.scanner.Scan() {
		line := s.scanner.Text()
		if len(line) == 0 {
			s.lastError = ""
			return
		}
		switch line[:1] {
		case "?":
			clearScreen()
			fmt.Println("[day]: switch to tag corresponding to [day]")
			fmt.Println("h: jump to today's month")
			fmt.Println("<: move backwards one month")
			fmt.Println(">: move forwards one month")
			fmt.Println("q or [enter]: exit")
			fmt.Println("")
			fmt.Println("press [enter] to continue...")
			s.scanner.Scan()
		case "<":
			currentDate = currentDate.AddDate(0, -1, 0)
		case ">":
			currentDate = currentDate.AddDate(0, 1, 0)
		case "h":
			currentDate = time.Now()
		case "q":
			s.lastError = ""
			return
		default:
			// try to parse as day
			i, err := strconv.Atoi(line)
			if err != nil {
				s.lastError = "invalid input"
				return
			}
			currentDate = time.Date(currentDate.Year(), currentDate.Month(), i, 0, 0, 0, 0, currentDate.Location())

			s.GoToDate(currentDate)
			s.lastError = ""
			return
		}

		s.printMonthCalendar(currentDate)
		fmt.Printf("\n=> ")
	}
}

func (s *state) MoveDays(num int) {
	curDate, err := time.Parse("January 02 2006", s.CurrentDBTag.Name)
	if err != nil {
		s.lastError = "not currently on date tag"
		return
	}

	s.GoToDate(curDate.AddDate(0, 0, num))
}

func (s *state) NewRow(arg string) (db.Row, bool) {
	var newRowText []byte
	var row db.Row
	var ok bool

	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		newRowText, ok = GetTextFromEditor(nil)
		if !ok {
			s.lastError = "editor exited abnormally"
			return row, false
		}
		if len(newRowText) == 0 {
			s.lastError = "empty input"
			return row, false
		}
	} else {
		newRowText = []byte(arg)
	}

	row, err := s.DB.AddRow(s.CurrentDBTag.ID, string(newRowText), 0)
	checkErr(err)

	s.lastError = ""
	s.Refresh()

	return row, true
}

func (s *state) InsertRow(arg string) {
	row, ok := s.NewRow(arg)
	if !ok {
		return
	}

	s.DB.UpdateRowRank(row.ID, 0)
	s.Refresh()
}

func (s *state) DeleteRows(arg string) {
	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		s.lastError = "[d]elete <row|row-range>[,<row|row-range>,...]"
		return
	}

	ok := s.CopyRows(arg)
	if !ok {
		// CopyRows will have set lastError
		return
	}

	for _, row := range s.snarfedRows {
		err := s.DB.DeleteRowByID(row.ID)
		checkErr(err)
	}

	s.lastError = fmt.Sprintf("cut %d rows", len(s.snarfedRows))
	s.Refresh()
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

	args := strings.Fields(arg)
	// if we don't have 2 distinct args, and our full string isn't exactly 2 chars (move ab)
	if len(args) != 2 && len(arg) != 2 {
		s.lastError = "move <a> <b>"
		return
	}
	if len(s.CurrentDBRows) == 0 {
		s.lastError = "no rows"
		return
	}
	// handle the move ab case
	if len(arg) == 2 {
		args[0] = string(arg[0])
		args = append(args, string(arg[1]))
	}
	// range check
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

func (s *state) SelectRowRange(arg string) ([]db.Row, bool) {
	var selectedRows []db.Row

	arg = strings.TrimSpace(arg)

	rowRange := strings.Split(arg, "-")
	if len(rowRange) != 2 {
		s.lastError = "invalid range specified"
		return nil, false
	}

	left := strings.TrimSpace(rowRange[0])
	right := strings.TrimSpace(rowRange[1])

	// make sure left and right exist in our rows
	if _, ok := s.rowShortcuts[left]; !ok {
		s.lastError = fmt.Sprintf("%s out of range", left)
		return nil, false
	}
	if _, ok := s.rowShortcuts[right]; !ok {
		s.lastError = fmt.Sprintf("%s out of range", right)
		return nil, false
	}
	// if range is reversed, just flip it
	rangeReversed := false
	if left > right {
		left, right = right, left
		rangeReversed = true
	}

	// range is valid; we should be able to blindly copy now
	for key := NewIncrementingKey(left); keyToInt(key.String()) <= keyToInt(right); key.Increment() {
		selectedRows = append(selectedRows, s.rowShortcuts[key.String()])
	}
	if rangeReversed {
		// put your thang down flip it and reverse it
		for i := 0; i < len(selectedRows)/2; i++ {
			// flip it
			selectedRows[i], selectedRows[len(selectedRows)-1-i] = selectedRows[len(selectedRows)-1-i], selectedRows[i]
		}
	}
	return selectedRows, true
}

func (s *state) PasteRowsEnd() {
	resnarf := make([]db.Row, 0, len(s.snarfedRows))

	if len(s.snarfedRows) == 0 {
		s.lastError = "empty snarf buffer"
		return
	}

	for _, row := range s.snarfedRows {
		newRow, err := s.DB.AddRow(s.CurrentDBTag.ID, row.Text, 0)
		checkErr(err)
		resnarf = append(resnarf, newRow)
	}

	// have to update our snarf buffer since these pasted rows are technically new
	s.snarfedRows = resnarf

	s.lastError = ""
	s.Refresh()
}

func (s *state) PasteRowsStart() {
	s.PasteRowsEnd()

	for i := len(s.snarfedRows) - 1; i >= 0; i-- {
		err := s.DB.UpdateRowRank(s.snarfedRows[i].ID, 0)
		checkErr(err)
	}

	s.Refresh()
}

func (s *state) CopyRows(arg string) bool {
	arg = strings.TrimSpace(arg)
	if len(arg) == 0 {
		s.lastError = "[y]ank <row|row-range>[,<row|row-range>,...]"
		return false
	}

	args := strings.Split(arg, ",")

	alreadySnarfedRows := make(map[db.Row]bool)
	snarfedRows := make([]db.Row, 0)
	for _, r := range args {
		if strings.Contains(r, "-") {
			rows, ok := s.SelectRowRange(r)
			if !ok {
				// SelectRowRange() should have already set lastError
				return false
			}
			for _, row := range rows {
				if !alreadySnarfedRows[row] {
					snarfedRows = append(snarfedRows, row)
					alreadySnarfedRows[row] = true
				}
			}
		} else {
			rowShortcut := strings.TrimSpace(r)
			if row, ok := s.rowShortcuts[rowShortcut]; ok {
				if !alreadySnarfedRows[row] {
					snarfedRows = append(snarfedRows, row)
					alreadySnarfedRows[row] = true
				}
			} else {
				s.lastError = fmt.Sprintf("invalid row: %s", rowShortcut)
				return false
			}
		}
	}

	s.snarfedRows = snarfedRows

	s.lastError = fmt.Sprintf("snarfed %d rows", len(s.snarfedRows))
	return true
}

func (s *state) printHelp() {
	clearScreen()
	fmt.Println("[Tags]")
	fmt.Println("h: jump to today tag ('h'ome)")
	fmt.Println("t: open all tags menu ('t'ags)")
	fmt.Println("g: open date picker ('g'oto)")
	fmt.Println("<: go back one day (left)")
	fmt.Println(">: go forward one day (right)")
	fmt.Println("t/<text>: search tag names for <text>")
	fmt.Println("t <text>: jump to or create to exact tag <text>")
	fmt.Println("r [text]: rename current tag with text <text> ('r'ename)")
	fmt.Println("")
	fmt.Println("[Rows]")
	fmt.Println("[num]: jump to row-referenced tag")
	fmt.Println("a [text]: add new row with text [text] or fire up editor if [text] is not present ('a'dd)")
	fmt.Println("A [text]: add new row in first row slot with text [text] or fire up editor if [text] is not present ('A'dd)")
	fmt.Println("d <row|row-range>[,<row|row-range>,...]: cut row(s) to snarf buffer ('d'elete)")
	fmt.Println("e <row>: edit row ('e'dit)")
	fmt.Println("m <row1> <row2>: move row1 to row2 ('m'ove)")
	fmt.Println("y <row|row-range>[,<row|row-range>,...]: yank row(s) to snarf buffer ('y'ank)")
	fmt.Println("p: paste snarfed rows to end of current tag ('p'aste)")
	fmt.Println("P: paste snarfed rows to beginning of current tag ('P'aste)")
	fmt.Println("?: print help")
	fmt.Println("")
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
	programState.lastError = "enter '?' for help"
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
		case 'A':
			programState.InsertRow(line[1:])
		case 'd':
			programState.DeleteRows(line[1:])
		case 'e':
			programState.EditRow(line[1:])
		case 'g':
			programState.PickDateInteractive()
		case 'm':
			programState.MoveRow(line[1:])
		case 'n':
			programState.NewTag(line[1:])
		case 't':
			programState.SelectTag(line[1:])
		case 'p':
			programState.PasteRowsEnd()
		case 'P':
			programState.PasteRowsStart()
		case 'r':
			programState.RenameTag(line[1:])
		case 'y':
			programState.CopyRows(line[1:])
		case '<':
			programState.MoveDays(-1)
		case '>':
			programState.MoveDays(1)
		case '?':
			programState.printHelp()
		case 'q':
			goto End
		default:
			// try to parse as int
			i, err := strconv.Atoi(line)
			if err != nil {
				programState.lastError = fmt.Sprintf("invalid command: %c", line[0])
				break
			}
			if tag, ok := programState.tagShortcutsRev[i]; ok {
				programState.lastError = ""
				programState.SwitchTag(tag)
			} else {
				programState.lastError = "no such tag ref"
			}
		}
		programState.RenderMain()
	}
End:
	programState.DeleteTagIfEmpty(programState.CurrentDBTag.ID)
	fmt.Println("bye!")
}
