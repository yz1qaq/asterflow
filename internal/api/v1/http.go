package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
)

const maxJSONBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(
		http.MaxBytesReader(w, r.Body, maxJSONBodyBytes),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

// 解析参数把字符串转换为int32
func pageParams(r *http.Request) (int32, int32, error) {
	limit, err := queryInt32(r, "limit", 50, 1, 200)
	if err != nil {
		return 0, 0, err
	}

	offset, err := queryInt32(r, "offset", 0, 0, math.MaxInt32)
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

func queryInt32(
	r *http.Request,
	name string,
	defaultValue, minValue, maxValue int32,
) (int32, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < int64(minValue) || value > int64(maxValue) {
		return 0, fmt.Errorf(
			"%s must be between %d and %d",
			name, minValue, maxValue,
		)
	}
	return int32(value), nil
}
