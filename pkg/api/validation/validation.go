// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"net/netip"

	"github.com/ironcore-dev/machine-controller-manager-provider-metal/pkg/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateProviderSpecAndSecret validates the provider spec and provider secret
func ValidateProviderSpecAndSecret(spec *v1alpha1.ProviderSpec, secret *corev1.Secret, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	allErrs = validateMachineClassSpec(spec, field.NewPath("spec"))
	allErrs = append(allErrs, validateSecret(secret, field.NewPath("spec"))...)

	return allErrs
}

func validateSecret(secret *corev1.Secret, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if secret == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("secretRef"), "secretRef is required"))
		return allErrs
	}

	if secret.Data["userData"] == nil {
		allErrs = append(allErrs, field.Required(field.NewPath("userData"), "userData is required"))
	}

	return allErrs
}

func validateMachineClassSpec(spec *v1alpha1.ProviderSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if spec.Image == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("image"), "image is required"))
	}

	for i, ip := range spec.DnsServers {
		if !netip.Addr.IsValid(ip) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dnsServers").Index(i), ip, "ip is invalid"))
		}
	}

	return allErrs
}
