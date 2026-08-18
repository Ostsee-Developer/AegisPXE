package trust

import "github.com/Ostsee-Developer/AegisPXE/internal/machine"

type Gate struct {
	OperatorApproved      bool
	AssignmentArmed       bool
	CryptographicVerified bool
	PublicBootAllowed     bool
	SecretReleaseAllowed  bool
	Reason                string
}

func Evaluate(policy machine.Policy, assignmentArmed, cryptographicVerified bool) Gate {
	gate := Gate{
		OperatorApproved:      policy == machine.PolicyProvision,
		AssignmentArmed:       assignmentArmed,
		CryptographicVerified: cryptographicVerified,
	}
	if !gate.OperatorApproved {
		gate.Reason = "operator_approval_required"
		return gate
	}
	if !gate.AssignmentArmed {
		gate.Reason = "assignment_required"
		return gate
	}
	gate.PublicBootAllowed = true
	if !gate.CryptographicVerified {
		gate.Reason = "cryptographic_boot_trust_required"
		return gate
	}
	gate.SecretReleaseAllowed = true
	gate.Reason = "trusted"
	return gate
}
