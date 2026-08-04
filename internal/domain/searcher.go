package domain

import (
	"fmt"
)

var (
	ErrWrongSelection = fmt.Errorf("wrong selection")
	ErrEmptySelection = fmt.Errorf("empty selection")
)

type Window[T any] interface {
	BindKey(key, description string)
	Cwd(cwd string)
	Preview(command string)
	ShowColumns(columns string)
	Display(rows []string) (T, string, error)
}

type SearchItemKind string

const (
	SearchItemKindSession   SearchItemKind = "SESSION"
	SearchItemKindDirectory SearchItemKind = "DIRECTORY"
)

type SearchItem interface {
	GetSessionName() string
	GetFullPath() string
	Parse(row string) (SearchItem, error)
}
