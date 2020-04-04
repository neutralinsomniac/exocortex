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

	sort.Slice(s.SortedRefTagsKeys, func(i, j int) bool { return s.SortedRefTagsKeys[i].UpdatedTS > s.SortedRefTagsKeys[j].UpdatedTS })

End:
	return err
}

func (s *State) DeleteTagIfEmpty(id int64) error {
	var rows []Row
	var refs Refs
	var err error

	rows, err = s.DB.GetRowsForTagID(id)
	if err != nil {
		goto End
	}

	refs, err = s.DB.GetRefsToTagByTagID(id)
	if err != nil {
		goto End
	}

	if len(rows)+len(refs) == 0 {
		err = s.DB.DeleteTagByID(id)
	}

End:
	return err
}
