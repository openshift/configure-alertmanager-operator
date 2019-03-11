package controller

import (
	"github.com/openshift/configure-alertmanager-operator/pkg/controller/secret"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, secret.Add)
}
