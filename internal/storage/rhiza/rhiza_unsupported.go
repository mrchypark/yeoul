//go:build !linux && !darwin

package rhiza

import (
	"context"
)

type DB struct{}

func Supported() bool { return false }

func Open(string) (*DB, error) { return nil, ErrUnsupported }

func (*DB) Close() error { return ErrUnsupported }

func (*DB) Apply(context.Context, string, []Record) error { return ErrUnsupported }

func (*DB) Records(context.Context) ([]Record, error) { return nil, ErrUnsupported }

func (*DB) Search(context.Context, string) ([]Record, error) { return nil, ErrUnsupported }
