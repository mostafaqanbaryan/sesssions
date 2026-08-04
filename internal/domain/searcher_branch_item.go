package domain

import (
	"path"
	"regexp"
	"strings"
)

type SearchBranchItem struct {
	remote bool
	name   string
	parent string
}

func NewSearchBranchItem(name, parent string, remote bool) SearchBranchItem {
	name = strings.TrimSpace(name)
	parent = strings.TrimSpace(parent)
	return SearchBranchItem{
		remote: remote,
		name:   name,
		parent: parent,
	}
}

func (s SearchBranchItem) GetSessionName() string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	return path.Base(s.parent) + "__" + re.ReplaceAllString(s.name, "-")
}

func (s SearchBranchItem) Parse(row string) (SearchItem, error) {
	params := strings.Split(row, "\t")
	if len(params) != 3 {
		return SearchBranchItem{}, ErrWrongSelection
	}

	return NewSearchBranchItem(params[1], params[2], false), nil
}

func (s SearchBranchItem) IsRemote() bool {
	return s.remote
}

func (s SearchBranchItem) GetBranchName() string {
	return s.name
}

func (s SearchBranchItem) GetFullPath() string {
	return path.Join(s.parent, "..", s.GetSessionName())
}
