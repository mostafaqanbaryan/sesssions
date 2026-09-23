package domain

import (
	"strings"
)

// SearchLanguageItem is a selectable language label in the fzf picker.
type SearchLanguageItem struct {
	lang string
}

func NewSearchLanguageItem(name string) SearchLanguageItem {
	return SearchLanguageItem{lang: strings.TrimSpace(name)}
}

func (s SearchLanguageItem) GetLanguage() string {
	return s.lang
}

func (s SearchLanguageItem) GetSessionName() string {
	return s.lang
}

func (s SearchLanguageItem) Parse(row string) (SearchItem, error) {
	return NewSearchLanguageItem(row), nil
}

func (s SearchLanguageItem) GetFullPath() string {
	return ""
}