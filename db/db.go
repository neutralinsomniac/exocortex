package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type ExoDB struct {
	conn        *sql.DB
	tx          *sql.Tx
	tx_refcount int
}

func (e *ExoDB) LoadSchema() error {
	_, err := e.conn.Exec(schema)
	return err
}

func (e *ExoDB) Open(filename string) error {
	var err error
	e.conn, err = sql.Open("sqlite3", filename)
	return err
}

func (e *ExoDB) Close() {
	e.tx_refcount = 0
	e.tx = nil
	e.conn.Close()
}

func (e *ExoDB) incTxRefCount() error {
	var err error

	if e.tx == nil {
		e.tx, err = e.conn.Begin()
	}

	if err != nil {
		panic("e.conn.Begin returned: " + err.Error())
	}

	e.tx_refcount++

	return err
}

func (e *ExoDB) decTxRefCount(commit bool) error {
	var err error

	if e.tx_refcount <= 0 {
		fmt.Println("decTxRefCount() called with refcount ==", e.tx_refcount)
	}

	e.tx_refcount--

	// always rollback if we're called with commit == false (something went wrong)
	if e.tx_refcount == 0 || commit == false {
		if commit == true {
			err = e.tx.Commit()
			e.tx = nil
		} else {
			err = e.tx.Rollback()
			e.tx = nil
		}
	}

	return err
}
