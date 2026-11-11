package render

import (
	"errors"
	"fmt"
)

var (
	errOddArguments = errors.New("expecting even number of arguments")
	errNonStringKey = errors.New("expecting string for odd numbered arguments")
)

// Map creates a map with the provided arg pairs.
func Map(args ...any) (map[string]any, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("%w, got %d", errOddArguments, len(args))
	}

	m := make(map[string]any)
	fn := ""

	for _, v := range args {
		if fn == "" {
			if s, ok := v.(string); ok {
				fn = s

				continue
			}

			return m, fmt.Errorf("%w, got %+v", errNonStringKey, v)
		}

		m[fn] = v
		fn = ""
	}

	return m, nil
}

// Arr returns a slice for a given argument list.
func Arr(args ...any) []any {
	return args
}

// Extend extends the target map with the provided arg pairs.
func Extend(target map[string]any, args ...any) (map[string]any, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("%w, got %d", errOddArguments, len(args))
	}

	fn := ""

	for _, v := range args {
		if fn == "" {
			if s, ok := v.(string); ok {
				fn = s

				continue
			}

			return target, fmt.Errorf("%w, got %+v", errNonStringKey, v)
		}

		target[fn] = v
		fn = ""
	}

	return target, nil
}

// FirstNotNil returns th first parameter that isn't nil.
func FirstNotNil(args ...any) any {
	for _, v := range args {
		if v != nil {
			return v
		}
	}

	return nil
}
