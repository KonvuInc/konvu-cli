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
// return for the given filter params.
//
// Fast path: send per_page=1 and read the response's "total" field, if the
// endpoint returns pagination metadata (container_findings, secret_findings,
// detections).
//
// Fallback: some endpoints — notably /sca_findings — do not return a total.
// We then walk pages with per_page=500 until a short page tells us we're
// done, counting items along the way. The caller's params are copied;
// per_page and page are overridden on each call.
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
	// Fallback: page-walk from page=1 with a bigger page size. The probe
	// response is thrown away — its per_page=1 shape means we can't reuse
	// its items to seed the count without under-counting the actual page 1.
	const pageSize = 500
	call["per_page"] = pageSize
	total := 0
	for page := 1; ; page++ {
		call["page"] = page
		resp, err := client.Get(endpoint, call)
		if err != nil {
			return 0, err
		}
		items, _ := resp["items"].([]any)
		total += len(items)
		if len(items) < pageSize {
			return total, nil
		}
	}
}
