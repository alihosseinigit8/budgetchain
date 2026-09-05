// Small contract helpers live here to keep the main contract logic readable.
package contract

import "strconv"

func formatUint(value uint64) string { return strconv.FormatUint(value, 10) }
