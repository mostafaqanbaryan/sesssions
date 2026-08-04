package fzf

import (
	"os/exec"

	"github.com/mostafaqanbaryan/sesssions/internal/domain"
)

type Fzf[T domain.SearchItem] struct{}

func NewFzf[T domain.SearchItem]() *Fzf[T] {
	if _, err := exec.LookPath("fzf"); err != nil {
		panic("fzf is required")
	}

	return &Fzf[T]{}
}

func (f Fzf[T]) NewWindow() domain.Window[T] {
	return NewFzfWindow[T]()
}
