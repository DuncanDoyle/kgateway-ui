package health

// Row reason kinds (D-063).
const (
	// ReasonFinding: the row's own error or warning finding decides its state.
	ReasonFinding = "finding"
	// ReasonUnknown: a required check is not OK, which makes every row unknown.
	ReasonUnknown = "unknown"
	// ReasonGatewayRejecting: the row is warning only because its Gateway rejects config.
	ReasonGatewayRejecting = "gateway-rejecting"
)

// RowReason says why a list or overview row is not healthy; healthy rows have
// none. Kind finding carries the deciding finding's code and message, never
// its detail or evidence, and More counts the object's other findings, all of
// which are of that severity or lower. The other kinds carry nothing else.
type RowReason struct {
	Kind    string `json:"kind"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	More    int    `json:"more,omitempty"`
}
