package support

import (
	"fmt"
	"time"
)

type ConditionFunc func() (bool, error)

func PollUntil(interval time.Duration, timeout time.Duration, fn ConditionFunc) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ok, err := fn()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		time.Sleep(interval)
	}

	return fmt.Errorf("timeout after %v", timeout)
}

func PollUntilOK(interval time.Duration, timeout time.Duration, fn func() error) error {
	return PollUntil(interval, timeout, func() (bool, error) {
		err := fn()
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}
