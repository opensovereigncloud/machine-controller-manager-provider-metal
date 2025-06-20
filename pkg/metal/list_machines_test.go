// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metal

import (
	"fmt"

	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gardener/machine-controller-manager/pkg/util/provider/driver"
	"github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/api/v1alpha1"
	"github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/cmd"
	"github.com/ironcore-dev/machine-controller-manager-provider-ironcore-metal/pkg/metal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("ListMachines", func() {
	ns, providerSecret, drv := SetupTest(cmd.NodeNamePolicyServerClaimName)

	It("should fail if no provider has been set", func(ctx SpecContext) {
		By("ensuring an error if no provider has been set")
		_, err := (*drv).ListMachines(ctx, &driver.ListMachinesRequest{
			MachineClass: newMachineClass("", testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).To(HaveOccurred())
	})

	It("should list no machines if none have been created", func(ctx SpecContext) {
		By("ensuring the list response contains no machines")
		listMachineResponse, err := (*drv).ListMachines(ctx, &driver.ListMachinesRequest{
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(listMachineResponse.MachineList).To(Equal(map[string]string{}))
	})

	It("should list a single machine if one has been created", func(ctx SpecContext) {
		machineName := "machine-0"
		By("creating a server")
		server := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-server",
			},
			Spec: metalv1alpha1.ServerSpec{
				SystemUUID: "12345",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server)

		By("starting a non-blocking goroutine to patch ServerClaim")
		go func() {
			defer GinkgoRecover()
			serverClaim := &metalv1alpha1.ServerClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      machineName,
				},
			}
			Eventually(Update(serverClaim, func() {
				serverClaim.Spec.ServerRef = &corev1.LocalObjectReference{Name: server.Name}
			})).Should(Succeed())
		}()

		By("creating a machine")
		craeteMachineResponse, err := (*drv).CreateMachine(ctx, &driver.CreateMachineRequest{
			Machine:      newMachine(ns, "machine", -1, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(craeteMachineResponse).To(Equal(&driver.CreateMachineResponse{
			ProviderID: fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 0),
			NodeName:   "machine-0",
		}))

		By("ensuring the list response contains the correct machine")
		listMachineResponse, err := (*drv).ListMachines(ctx, &driver.ListMachinesRequest{
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(listMachineResponse.MachineList).To(Equal(
			map[string]string{fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 0): "machine-0"},
		))

		By("ensuring the cleanup of the machine")
		DeferCleanup((*drv).DeleteMachine, &driver.DeleteMachineRequest{
			Machine:      newMachine(ns, "machine", -1, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
	})

	It("should list two machines if two have been created", func(ctx SpecContext) {
		machineName := "machine-0"
		machineName2 := "machine-1"
		By("creating a server")
		server := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-server",
			},
			Spec: metalv1alpha1.ServerSpec{
				SystemUUID: "12345",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server)

		By("creating a server")
		server2 := &metalv1alpha1.Server{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-server-2",
			},
			Spec: metalv1alpha1.ServerSpec{
				SystemUUID: "12345",
			},
		}
		Expect(k8sClient.Create(ctx, server2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, server2)

		By("starting a non-blocking goroutine to patch ServerClaim")
		go func() {
			defer GinkgoRecover()
			serverClaim := &metalv1alpha1.ServerClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      machineName,
				},
			}
			Eventually(Update(serverClaim, func() {
				serverClaim.Spec.ServerRef = &corev1.LocalObjectReference{Name: server.Name}
			})).Should(Succeed())
		}()

		By("creating the first machine")
		craeteMachineResponse, err := (*drv).CreateMachine(ctx, &driver.CreateMachineRequest{
			Machine:      newMachine(ns, "machine", 0, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(craeteMachineResponse).To(Equal(&driver.CreateMachineResponse{
			ProviderID: fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 0),
			NodeName:   machineName,
		}))

		go func() {
			defer GinkgoRecover()
			serverClaim := &metalv1alpha1.ServerClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Name:      machineName2,
				},
			}
			Eventually(Update(serverClaim, func() {
				serverClaim.Spec.ServerRef = &corev1.LocalObjectReference{Name: server2.Name}
			})).Should(Succeed())
		}()

		By("creating the second machine")
		craeteMachineResponse, err = (*drv).CreateMachine(ctx, &driver.CreateMachineRequest{
			Machine:      newMachine(ns, "machine", 1, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(craeteMachineResponse).To(Equal(&driver.CreateMachineResponse{
			ProviderID: fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 1),
			NodeName:   machineName2,
		}))

		By("ensuring the machine status contains 2 machines")
		listMachinesResponse, err := (*drv).ListMachines(ctx, &driver.ListMachinesRequest{
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(listMachinesResponse.MachineList).To(Equal(map[string]string{
			fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 0): machineName,
			fmt.Sprintf("%s://%s/machine-%d", v1alpha1.ProviderName, ns.Name, 1): machineName2,
		}))

		By("ensuring the cleanup of the first machine")
		DeferCleanup((*drv).DeleteMachine, &driver.DeleteMachineRequest{
			Machine:      newMachine(ns, "machine", 0, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})

		By("ensuring the cleanup of the second machine")
		DeferCleanup((*drv).DeleteMachine, &driver.DeleteMachineRequest{
			Machine:      newMachine(ns, "machine", 1, nil),
			MachineClass: newMachineClass(v1alpha1.ProviderName, testing.SampleProviderSpec),
			Secret:       providerSecret,
		})
	})
})
