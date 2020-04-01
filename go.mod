module github.com/neutralinsomniac/exocortex

go 1.13

require (
	gioui.org v0.0.0-20200217143708-0dd77be97543
	github.com/mattn/go-sqlite3 v2.0.3+incompatible
	github.com/mjl-/duit v0.0.0-20200330125617-580cb0b2843f
	golang.org/x/exp v0.0.0-20200213203834-85f925bdd4d0 // indirect
	golang.org/x/image v0.0.0-20200119044424-58c23975cae1 // indirect
	golang.org/x/sys v0.0.0-20200219091948-cb0a6d8edb6c // indirect
	golang.org/x/text v0.3.2 // indirect
)

replace gioui.org => github.com/neutralinsomniac/gio v0.0.0-20200219144250-32608877c821

replace github.com/mjl-/duit => ../duit

replace 9fans.net/go v0.0.0-00010101000000-000000000000 => github.com/mjl-/go v0.0.0-20180429123528-fafada5f286e
