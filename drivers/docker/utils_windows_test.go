// +build windows

package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandPath(t *testing.T) {
	cases := []struct {
		base     string
		target   string
		expected string
	}{
		{"/tmp/alloc/task", "/home/user", "\\home\\user"},
		{"/tmp/alloc/task", "/home/user/..", "\\home"},

		{"/tmp/alloc/task", ".", "\\tmp\\alloc\\task"},
		{"/tmp/alloc/task", "..", "\\tmp\\alloc"},

		{"/tmp/alloc/task", "d1/d2", "\\tmp\\alloc\\task\\d1\\d2"},
		{"/tmp/alloc/task", "../d1/d2", "\\tmp\\alloc\\d1\\d2"},
		{"/tmp/alloc/task", "../../d1/d2", "\\tmp\\d1\\d2"},

		{"\\tmp\\alloc\\task", "d1\\d2", "\\tmp\\alloc\\task\\d1\\d2"},
		{"\\tmp\\alloc\\task", "..\\d1\\d2", "\\tmp\\alloc\\d1\\d2"},
		{"\\tmp\\alloc\\task", "..\\..\\d1\\d2", "\\tmp\\d1\\d2"},
	}

	for _, c := range cases {
		t.Run(c.expected, func(t *testing.T) {
			require.Equal(t, c.expected, expandPath(c.base, c.target))
		})
	}
}
