// The errors in this file define the contract input boundary. They are returned
// before block creation so invalid transactions never enter the ledger.
package contract

import "errors"

var (
	ErrUnknownMsg = errors.New("contract: unknown message")
	ErrInvalidMsg = errors.New("contract: invalid message")
	ErrNilKeeper  = errors.New("contract: keeper is nil")
)
