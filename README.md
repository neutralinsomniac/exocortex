# exocortex

A note-taking/information storage system/todo list/whatever-you-want-it-to-be written in Golang.

## Concepts

exocortex tries to be as friction-free as possible when it comes to enabling the user to write information down and not worry about precise organization beforehand.

The default view when starting exocortex is the tag for today's date, which encourages the user to not have to care about creating a tag for the information about to be stored beforehand. Just start typing, and add [[tags]] as they make sense.

Tags are the highest-level organizational structure in exocortex. They can be created explicitly using the new tag command, or created on-the-fly by referencing them while adding rows under the current tag. Tags are created on-the-fly by enclosing text in `[[ ]]` blocks in rows. For instance, `[[this]]` is a tag, `[[and so is this]]`. Tags are automatically deleted when the tag contains no more rows and no other rows reference the tag.

Rows are bullets that fall under a given tag. When a row references another tag, exocortex automatically links that row to the specified tag, in both directions. So for instance, if you are on the tag for today's date, and you add a row with the content "[[todo]] take out the trash", viewing the "todo" tag will show you a reference to the today tag, with the full text of the row available for viewing and/or editing.

## Installation

* The two most feature-complete frontends are currently **exotui** (a text-ui) and **exogio** (a graphical frontend using [gioui](https://gioui.org)). **exotui** implements the most complete featureset and is currently the recommended interface to use. Both frontends use the exact same database code, so they are compatible with eachother and multiple instances of either client can be run at the same time targetting the same database.

* Grab a release binary from [Releases](https://github.com/neutralinsomniac/exocortex/releases)
OR
* for **exotui**: `go get -u github.com/neutralinsomniac/exocortex/cmd/exotui`
* for **exogio**: `go get -u github.com/neutralinsomniac/exocortex/cmd/exogio`

## Usage

Just run `exotui` or `exogio`.

Currently, all persistent data is stored in a sqlite3 database called "exocortex.db" in the directory where exocortex is started from. This is intentional as it allows you to create different databases for different projects/contexts/whatever. I like to keep my main personal database in my home directory, and keep separate databases under the various project directories I'm working in.

## Usage:

### exotui

Enter "?" for in-app help

### exogio

Click any row to edit it.

Click any tag to jump to it.

Click on any tag header in the "References" section to jump to that tag.

Tab: Switch focus between the New Row editor and the Filter/New Tag editor.

Escape: Clear the current editor field. If editing a row, press escape once to clear the row, then escape again to cancel the edit and revert the row to its un-edited state.

Enter: Submit the current field. In the New Row editor, add a new row. In the Filter/New Tag editor, either create a new tag if it doesn't exist or jump to the specified tag if it does exist.

To delete a row, first click on it to start editing, then hit Escape to clear the row, then Enter to submit the cleared row, which deletes it.

#### exogio roadmap

- Tag autocomplete
- Allow rows to be rearranged
- Row hierarchy/indentation
- Copy/paste + selection (currently this is limited by the GUI project exocortex uses: [gio](https://gioui.org/))
- Customizable database storage
- Multiple-tag filtering
- Tag merging when renaming a tag onto an existing tag
- Date picker
