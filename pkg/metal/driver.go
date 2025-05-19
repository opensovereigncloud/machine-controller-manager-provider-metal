// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metal

import (
	"context"
	"fmt"

	"github.com/gardener/machine-controller-manager/pkg/util/provider/driver"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/codes"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/status"
	apiv1alpha1 "github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/api/v1alpha1"
	mcmclient "github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/client"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultIgnitionKey     = "ignition"
	ShootNameLabelKey      = "shoot-name"
	ShootNamespaceLabelKey = "shoot-namespace"
)

var (
	fieldOwner = client.FieldOwner("mcm.ironcore.dev/field-owner")
)

type metalDriver struct {
	Schema                  *runtime.Scheme
	clientProvider          *mcmclient.Provider
	metalNamespace          string
	useServerNameAsNodeName bool
}

func (d *metalDriver) InitializeMachine(ctx context.Context, request *driver.InitializeMachineRequest) (*driver.InitializeMachineResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Metal Provider does not yet implement InitializeMachine")
}

func (d *metalDriver) GetVolumeIDs(_ context.Context, req *driver.GetVolumeIDsRequest) (*driver.GetVolumeIDsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Metal Provider does not yet implement GetVolumeIDs")
}

// NewDriver returns a new Gardener metal driver object
func NewDriver(cp *mcmclient.Provider, namespace string, useServerNameAsMachineID bool) driver.Driver {
	return &metalDriver{
		clientProvider:          cp,
		metalNamespace:          namespace,
		useServerNameAsNodeName: useServerNameAsMachineID,
	}
}

func (d *metalDriver) GenerateMachineClassForMigration(_ context.Context, _ *driver.GenerateMachineClassForMigrationRequest) (*driver.GenerateMachineClassForMigrationResponse, error) {
	return &driver.GenerateMachineClassForMigrationResponse{}, nil
}

func (d *metalDriver) getIgnitionNameForMachine(ctx context.Context, machineName string) string {
	//for backward compatibility checking if the ignition secret was already present with the old naming convention
	ignitionSecretName := fmt.Sprintf("%s-%s", machineName, "ignition")
	d.clientProvider.Lock()
	defer d.clientProvider.Unlock()
	if err := d.clientProvider.Client.Get(ctx, client.ObjectKey{Name: ignitionSecretName, Namespace: d.metalNamespace}, &corev1.Secret{}); apierrors.IsNotFound(err) {
		return machineName
	}
	return ignitionSecretName
}

func getProviderIDForServerClaim(serverClaim *metalv1alpha1.ServerClaim) string {
	return fmt.Sprintf("%s://%s/%s", apiv1alpha1.ProviderName, serverClaim.Namespace, serverClaim.Name)
}
