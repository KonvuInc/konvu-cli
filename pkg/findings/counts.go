// Package findings holds cobra-agnostic helpers shared across
// konvu-cli's finding subcommands (SCA, SAST, container, secrets).
package findings

// apiClient is the minimum surface CountByPagination needs from
// *api.Client. Kept local so pkg/findings has no dependency on pkg/api
// at this seam and can be exercised with a fake.
type apiClient interface {
	Get(path string, params map[string]any) (map[string]any, error)
}

// CountByPagination returns the total number of rows the endpoint would
// return for the given filter params. Sends per_page=1 and reads the
// response's "total" field. Returns 0 if the endpoint omits pagination
// metadata. The caller's params are copied; per_page and page are
// overridden.
func CountByPagination(client apiClient, endpoint string, params map[string]any) (int, error) {
	call := make(map[string]any, len(params)+2)
	for k, v := range params {
		call[k] = v
	}
	call["per_page"] = 1
	call["page"] = 1

	resp, err := client.Get(endpoint, call)
	if err != nil {
		return 0, err
	}
	switch t := resp["total"].(type) {
	case float64:
		return int(t), nil
	case int:
		return t, nil
	}
	return 0, nil
}
