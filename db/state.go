package db

import (
	"sort"
)

type State struct {
	DB                *ExoDB
	AllDBTags         []Tag
	CurrentDBTag      Tag
	CurrentDBRows     []Row
	CurrentDBRefs     Refs
	SortedRefTagsKeys []Tag
}

func (s *State) Refresh() error {
	var err error
	var i int

	s.AllDBTags, err = s.DB.GetAllTags()
	if err != nil {
		goto End
	}

	s.CurrentDBRows, err = s.DB.GetRowsForTagID(s.CurrentDBTag.ID)
	if err != nil {
		goto End
	}

	// refs
	s.CurrentDBRefs, err = s.DB.GetRefsToTagByTagID(s.CurrentDBTag.ID)

	// sorted ref keys
	s.SortedRefTagsKeys = make([]Tag, len(s.CurrentDBRefs))
	i = 0
	for k := range s.CurrentDBRefs {
		s.SortedRefTagsKeys[i] = k
		i++
	}

	sort.Slice(s.SortedRefTagsKeys, func(i, j int) bool { return s.SortedRefTagsKeys[i].Name < s.SortedRefTagsKeys[j].Name })

End:
	return err
}
