// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metal

import (
	"context"
	"fmt"
	"net"

	apiv1alpha1 "github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/api/v1alpha1"
	"github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/api/validation"
	"github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/ignition"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"

	"github.com/gardener/machine-controller-manager/pkg/util/provider/driver"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/codes"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/status"

	"github.com/imdario/mergo"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	capiv1beta1 "sigs.k8s.io/cluster-api/exp/ipam/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InitializeMachine handles a machine initialization request, which includes creating an ignition secret and powering on the server
func (d *metalDriver) InitializeMachine(ctx context.Context, req *driver.InitializeMachineRequest) (*driver.InitializeMachineResponse, error) {
	if isEmptyInitializeRequest(req) {
		return nil, status.Error(codes.InvalidArgument, "received empty InitializeMachineRequest")
	}

	if req.MachineClass.Provider != apiv1alpha1.ProviderName {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("requested provider %q is not supported by the driver %q", req.MachineClass.Provider, apiv1alpha1.ProviderName))
	}

	klog.V(3).Infof("machine initialization request has been received for %s", req.Machine.Name)
	defer klog.V(3).Infof("machine initialization request has been processed for %s", req.Machine.Name)

	providerSpec, err := GetProviderSpec(req.MachineClass, req.Secret)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get provider spec: %v", err))
	}

	addressesMetaData := make(map[string]any)
	unavailable, err := d.getIPAddressClaims(ctx, req, providerSpec, addressesMetaData)
	if err != nil {
		if unavailable {
			return nil, status.Error(codes.Unavailable, fmt.Sprintf("IPAddressClaim(s) still pending: %v", err))
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get IPAddressClaims: %v", err))
	}

	ignitionSecret, err := d.generateIgnitionSecret(ctx, req, req.Machine.Name, providerSpec, addressesMetaData, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to generate ignition secret: %v", err))
	}

	if err = d.createIgnitionSecret(ctx, ignitionSecret); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create ignition secret: %v", err))
	}

	serverClaim, unavailable, err := d.getServerClaim(ctx, req)
	if err != nil {
		if unavailable {
			return nil, status.Error(codes.Unavailable, fmt.Sprintf("failed to get ServerClaim %s/%s, not ready: %v", d.metalNamespace, req.Machine.Name, err))
		} else {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get ServerClaim: %v", err))
		}
	}

	if err := d.updateIgnitionAndPowerOnServer(ctx, req, serverClaim, providerSpec, addressesMetaData); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to update ignition and power on server: %v", err))
	}

	nodeName, err := getNodeName(ctx, d.nodeNamePolicy, serverClaim, d.metalNamespace, d.clientProvider)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get node name: %v", err))
	}

	return &driver.InitializeMachineResponse{
		ProviderID: getProviderIDForServerClaim(serverClaim),
		NodeName:   nodeName,
	}, nil
}

// isEmptyInitializeRequest checks if any of the fields in InitializeMachineRequest is empty
func isEmptyInitializeRequest(req *driver.InitializeMachineRequest) bool {
	return req == nil || req.MachineClass == nil || req.Machine == nil || req.Secret == nil
}

// getIPAddressClaims gets IPAddressClaims for IPAMConfigs in the providerSpec
func (d *metalDriver) getIPAddressClaims(ctx context.Context, req *driver.InitializeMachineRequest, providerSpec *apiv1alpha1.ProviderSpec, addressesMetaData map[string]any) (bool, error) {
	for _, ipamConfig := range providerSpec.IPAMConfig {
		ipAddrClaimName := getIPAddressClaimName(req.Machine.Name, ipamConfig.MetadataKey)
		ipAddrClaimKey := client.ObjectKey{Namespace: d.metalNamespace, Name: ipAddrClaimName}
		ipClaim := &capiv1beta1.IPAddressClaim{}

		if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
			return metalClient.Get(ctx, ipAddrClaimKey, ipClaim)
		}); err != nil {
			return false, err
		}

		klog.V(3).Info("validating IPAddressClaim", "namespace", ipAddrClaimKey.Namespace, "name", ipAddrClaimKey.Name)

		validationErr := validation.ValidateIPAddressClaim(ipClaim, d.metalNamespace, req.Machine.Name, ipAddrClaimKey)
		if validationErr.ToAggregate() != nil && len(validationErr.ToAggregate().Errors()) > 0 {
			return true, fmt.Errorf("failed to validate IPAddressClaim, still pending: %v", validationErr.ToAggregate().Errors())
		}

		ipAddrKey := client.ObjectKey{Namespace: ipClaim.Namespace, Name: ipClaim.Status.AddressRef.Name}
		ipAddr := &capiv1beta1.IPAddress{}
		if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
			return metalClient.Get(ctx, ipAddrKey, ipAddr)
		}); err != nil {
			return false, err
		}

		klog.V(3).Info("IP metadata found", "namespace", ipAddrKey.Namespace, "name", ipAddrKey.Name, "ip", ipAddr.Spec.Address, "prefix", ipAddr.Spec.Prefix, "gateway", ipAddr.Spec.Gateway)

		addressesMetaData[ipamConfig.MetadataKey] = map[string]any{
			"ip":      ipAddr.Spec.Address,
			"prefix":  ipAddr.Spec.Prefix,
			"gateway": ipAddr.Spec.Gateway,
		}

		klog.V(3).Info("IP address added to metadata", "namespace", ipAddrKey.Namespace, "name", ipAddrKey.Name)
	}

	klog.V(3).Info("successfully processed all IPs", "count", len(providerSpec.IPAMConfig))
	return false, nil
}

// generateIgnition creates an ignition file for the machine and stores it in a secret
func (d *metalDriver) generateIgnitionSecret(ctx context.Context, req *driver.InitializeMachineRequest, hostname string, providerSpec *apiv1alpha1.ProviderSpec, addressesMetaData map[string]any, serverMetadata *ServerMetadata) (*corev1.Secret, error) {
	userData, ok := req.Secret.Data["userData"]
	if !ok {
		return nil, fmt.Errorf("failed to find user-data in Secret %q", client.ObjectKeyFromObject(req.Secret))
	}

	if providerSpec.Metadata == nil {
		providerSpec.Metadata = make(map[string]any)
	}

	if serverMetadata != nil {
		metadata := map[string]any{}
		if serverMetadata.LoopbackAddress != nil {
			metadata["loopbackAddress"] = serverMetadata.LoopbackAddress.String()
		}
		if err := mergo.Merge(&providerSpec.Metadata, metadata, mergo.WithOverride); err != nil {
			return nil, fmt.Errorf("failed to merge server metadata into provider metadata: %w", err)
		}
	}

	if err := mergo.Merge(&providerSpec.Metadata, addressesMetaData, mergo.WithOverride); err != nil {
		return nil, fmt.Errorf("failed to merge addresses metadata into provider metadata: %w", err)
	}

	config := &ignition.Config{
		Hostname:         hostname,
		UserData:         string(userData),
		MetaData:         providerSpec.Metadata,
		Ignition:         providerSpec.Ignition,
		DnsServers:       providerSpec.DnsServers,
		IgnitionOverride: providerSpec.IgnitionOverride,
	}

	ignitionContent, err := ignition.Render(config)
	if err != nil {
		return nil, fmt.Errorf("failed to render ignition for Machine %q: %w", client.ObjectKeyFromObject(req.Machine), err)
	}

	ignitionData := map[string][]byte{}
	ignitionData["ignition"] = []byte(ignitionContent)
	ignitionSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.getIgnitionNameForMachine(ctx, req.Machine.Name),
			Namespace: d.metalNamespace,
		},
		Data: ignitionData,
	}

	return ignitionSecret, nil
}

// createIgnitionSecret creates and applies an Ignition Secret CR with proper ignition data
func (d *metalDriver) createIgnitionSecret(ctx context.Context, ignitionSecret *corev1.Secret) error {
	if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
		return metalClient.Patch(ctx, ignitionSecret, client.Apply, fieldOwner, client.ForceOwnership)
	}); err != nil {
		return fmt.Errorf("error applying ignition Secret: %w", err)
	}

	return nil
}

// updateIgnitionAndPowerOnServer updates the ignition secret in the ServerClaim and powers on the server
func (d *metalDriver) updateIgnitionAndPowerOnServer(ctx context.Context, req *driver.InitializeMachineRequest, serverClaim *metalv1alpha1.ServerClaim, providerSpec *apiv1alpha1.ProviderSpec, addressesMetaData map[string]any) error {
	nodeName, err := getNodeName(ctx, d.nodeNamePolicy, serverClaim, d.metalNamespace, d.clientProvider)
	if err != nil {
		return fmt.Errorf("failed to get node name: %w", err)
	}

	serverMetadata, err := d.extractServerMetadataFromClaim(ctx, serverClaim)
	if err != nil {
		return fmt.Errorf("error extracting server metadata from ServerClaim %q: %w", client.ObjectKeyFromObject(serverClaim), err)
	}

	ignitionSecret, err := d.generateIgnitionSecret(ctx, req, nodeName, providerSpec, addressesMetaData, serverMetadata)
	if err != nil {
		return err
	}

	if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
		return metalClient.Patch(ctx, ignitionSecret, client.Apply, fieldOwner, client.ForceOwnership)
	}); err != nil {
		return err
	}

	serverClaimBase := serverClaim.DeepCopy()
	serverClaim.Spec.Power = metalv1alpha1.PowerOn

	if err = d.clientProvider.ClientSynced(func(metalClient client.Client) error {
		return metalClient.Patch(ctx, serverClaim, client.MergeFrom(serverClaimBase))
	}); err != nil {
		return err
	}

	return nil
}

type ServerMetadata struct {
	LoopbackAddress net.IP
}

func (d *metalDriver) extractServerMetadataFromClaim(ctx context.Context, claim *metalv1alpha1.ServerClaim) (*ServerMetadata, error) {
	if claim.Spec.ServerRef == nil {
		return nil, fmt.Errorf("ServerClaim %q does not have a server reference", client.ObjectKeyFromObject(claim))
	}

	server := &metalv1alpha1.Server{}

	if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
		return metalClient.Get(ctx, client.ObjectKey{Name: claim.Spec.ServerRef.Name}, server)
	}); err != nil {
		return nil, fmt.Errorf("failed to get Server by reference %q: %w", claim.Spec.ServerRef.Name, err)
	}

	serverMetadata := &ServerMetadata{}

	loopbackAddress, ok := server.Annotations[apiv1alpha1.LoopbackAddressAnnotation]
	if ok {
		addr := net.ParseIP(loopbackAddress)
		if addr != nil {
			serverMetadata.LoopbackAddress = addr
		}
	}

	return serverMetadata, nil
}

func (d *metalDriver) getServerClaim(ctx context.Context, req *driver.InitializeMachineRequest) (*metalv1alpha1.ServerClaim, bool, error) {
	serverClaim := &metalv1alpha1.ServerClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Machine.Name,
			Namespace: d.metalNamespace,
		},
	}

	if err := d.clientProvider.ClientSynced(func(metalClient client.Client) error {
		return metalClient.Get(ctx, client.ObjectKeyFromObject(serverClaim), serverClaim)
	}); err != nil {
		return nil, false, fmt.Errorf("failed to get ServerClaim %q: %w", client.ObjectKeyFromObject(serverClaim), err)
	}

	if serverClaim.Spec.ServerRef == nil {
		return nil, true, fmt.Errorf("ServerClaim %q does not have a server reference", client.ObjectKeyFromObject(serverClaim))
	}

	return serverClaim, false, nil
}
