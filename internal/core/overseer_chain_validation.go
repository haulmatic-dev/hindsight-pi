package core

import (
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RunTDDValidation delegates to the validation service.
func (o *Overseer) RunTDDValidation(refactorBead types.Bead, projectPath string) error {
	return o.validationService.RunTDDValidation(refactorBead, projectPath)
}

// RunPRDValidation delegates to the validation service.
func (o *Overseer) RunPRDValidation(chainInfo *ChainInfo, projectPath string) error {
	return o.validationService.RunPRDValidation(chainInfo, projectPath)
}

// findTDDChainBeads delegates to the validation service.
func (o *Overseer) findTDDChainBeads(refactorBead types.Bead) (types.Bead, types.Bead, error) {
	return o.validationService.findTDDChainBeads(refactorBead)
}
