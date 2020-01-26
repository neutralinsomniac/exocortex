package db

type Ref struct {
	tag_id int64
	row_id int64
}

func (e *ExoDB) GetRefsToTagByTagName(name string) ([]Row, error) {
	var tag Tag
	var rows []Row
	var err error

	err = e.incTxRefCount()
	if err != nil {
		goto End
	}

	tag, err = e.GetTagByName(name)
	if err != nil {
		goto End
	}

	rows, err = e.GetRefsToTagByTagID(tag.id)
	if err != nil {
		goto End
	}

End:
	e.decTxRefCount(err == nil)

	return rows, err
}

func (e *ExoDB) GetRefsToTagByTagID(tag_id int64) ([]Row, error) {
	var rows []Row
	var err error

	return rows, err
}
