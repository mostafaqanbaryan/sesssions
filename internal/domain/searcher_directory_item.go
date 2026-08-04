package domain

import (
	"path"
	"strings"
)

type SearchDirectoryItem struct {
	kind   SearchItemKind
	name   string
	parent string
}

func NewSearchDirectoryItem(name, parent string, kind SearchItemKind) SearchDirectoryItem {
	name = strings.TrimSpace(name)
	parent = strings.TrimSpace(parent)
	return SearchDirectoryItem{
		name:   name,
		parent: parent,
		kind:   kind,
	}
}

func (s SearchDirectoryItem) GetSessionName() string {
	return s.name
}

func (s SearchDirectoryItem) Parse(row string) (SearchItem, error) {
	params := strings.Split(row, "\t")
	if len(params) != 5 {
		return SearchDirectoryItem{}, ErrWrongSelection
	}

	return NewSearchDirectoryItem(params[2], params[3], SearchItemKindDirectory), nil
}

func (s SearchDirectoryItem) GetDirName() string {
	return s.name
}

func (s SearchDirectoryItem) GetParentDir() string {
	return s.parent
}

func (s SearchDirectoryItem) GetFullPath() string {
	return path.Join(s.parent, s.name)
}

func (s SearchDirectoryItem) GetKind() string {
	if s.kind == SearchItemKindDirectory {
		return "Directory"
	}

	return "Session"
}
