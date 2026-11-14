//go:build !linux && !darwin

package crossseed

import "errors"

func reflink(from, to string) error {
	return errors.New("this system makes no reflinks; pick hardlinks or symlinks")
}
