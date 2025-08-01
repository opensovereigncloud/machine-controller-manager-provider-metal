// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metal

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/machine-controller-manager/pkg/util/provider/driver"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/codes"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/status"
	apiv1alpha1 "github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/api/v1alpha1"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteMachine handles a machine deletion request and also deletes ignitionSecret associated with it
func (d *metalDriver) DeleteMachine(ctx context.Context, req *driver.DeleteMachineRequest) (*driver.DeleteMachineResponse, error) {
	if isEmptyDeleteRequest(req) {
		return nil, status.Error(codes.InvalidArgument, "received empty DeleteMachineRequest")
	}

	if req.MachineClass.Provider != apiv1alpha1.ProviderName {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("requested provider %q is not supported by the driver %q", req.MachineClass.Provider, apiv1alpha1.ProviderName))
	}

	klog.V(3).Infof("Machine deletion request has been received for %q", req.Machine.Name)
	defer klog.V(3).Infof("Machine deletion request has been processed for %q", req.Machine.Name)

	ignitionSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.getIgnitionNameForMachine(ctx, req.Machine.Name),
			Namespace: d.metalNamespace,
		},
	}

	if err := d.clientProvider.SyncClient(func(metalClient client.Client) error {
		return metalClient.Delete(ctx, ignitionSecret)
	}); client.IgnoreNotFound(err) != nil {
		// Unknown leads to short retry in machine controller
		return nil, status.Error(codes.Unknown, fmt.Sprintf("error deleting ignition secret: %s", err.Error()))
	}

	serverClaim := &metalv1alpha1.ServerClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Machine.Name,
			Namespace: d.metalNamespace,
		},
	}

	if err := d.clientProvider.SyncClient(func(metalClient client.Client) error {
		return metalClient.Delete(ctx, serverClaim)
	}); err != nil {
		if !apierrors.IsNotFound(err) {
			// Unknown leads to short retry in machine controller
			return nil, status.Error(codes.Unknown, fmt.Sprintf("error deleting pod: %s", err.Error()))
		}
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Actively wait until the server claim is deleted since the extension contract in machine-controller-manager expects drivers to
	// do so. If we would not wait until the server claim is gone it might happen that the kubelet could re-register the Node
	// object even after it was already deleted by machine-controller-manager.
	if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := d.clientProvider.SyncClient(func(metalClient client.Client) error {
			return metalClient.Get(ctx, client.ObjectKeyFromObject(serverClaim), serverClaim)
		}); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			// Unknown leads to short retry in machine controller
			return false, status.Error(codes.Unknown, err.Error())
		}
		return false, nil
	}); err != nil {
		klog.V(3).Infof("Failed to wait for ServerClaim deletion: %v", err)
		// will be retried with short retry by machine controller
		return nil, status.Error(codes.DeadlineExceeded, err.Error())
	}

	klog.V(3).Infof("ServerClaim %q in namespace %q has been deleted", serverClaim.Name, serverClaim.Namespace)
	return &driver.DeleteMachineResponse{}, nil
}

func isEmptyDeleteRequest(req *driver.DeleteMachineRequest) bool {
	return req == nil || req.MachineClass == nil || req.Machine == nil || req.Secret == nil
}
