package registry

import (
	"fmt"
	"strings"
)

type DependencyChain []string

func (c DependencyChain) String() string {
	return strings.Join(c, " -> ")
}

func (c DependencyChain) With(pkg string) DependencyChain {
	newChain := make(DependencyChain, len(c)+1)
	copy(newChain, c)
	newChain[len(c)] = pkg
	return newChain
}

func (c DependencyChain) Err(err error) error {
	return fmt.Errorf("%s: %w", c.String(), err)
}
