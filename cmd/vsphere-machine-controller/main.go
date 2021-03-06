/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"github.com/golang/glog"
	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/util/logs"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clusterapiclientsetscheme "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/scheme"
	"sigs.k8s.io/cluster-api/pkg/controller/cluster"
	"sigs.k8s.io/cluster-api/pkg/controller/config"
	"sigs.k8s.io/cluster-api/pkg/controller/machine"
	"sigs.k8s.io/cluster-api/pkg/controller/sharedinformers"
)

var (
	namedMachinesPath = pflag.String("namedmachines", "", "path to named machines yaml file")
)

const vsphereMachineControllerName = "vsphere-machine-controller"

func init() {
	config.ControllerConfig.AddFlags(pflag.CommandLine)
}

func main() {
	pflag.Parse()

	logs.InitLogs()
	defer logs.FlushLogs()

	config, err := controller.GetConfig(config.ControllerConfig.Kubeconfig)
	if err != nil {
		glog.Fatalf("Could not create Config for talking to the apiserver: %v", err)
	}

	client, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	machineClientSet, err := kubernetes.NewForConfig(
		rest.AddUserAgent(config, "machine-controller-manager"),
	)
	if err != nil {
		glog.Fatalf("Invalid API configuration for kubeconfig-control: %v", err)
	}

	clusterClientSet, err := kubernetes.NewForConfig(
		rest.AddUserAgent(config, "cluster-controller-manager"),
	)
	if err != nil {
		glog.Fatalf("Invalid API configuration for kubeconfig-control: %v", err)
	}

	machineEventRecorder, err := createRecorder(machineClientSet, "machine-controller-manager")
	if err != nil {
		glog.Fatalf("Could not create vSphere event recorder: %v", err)
	}

	shutdown := make(chan struct{})
	si := sharedinformers.NewSharedInformers(config, shutdown)

	// machineActuator, err := vsphere.NewTerraformMachineActuator(client.ClusterV1alpha1(), si.Factory.Cluster().V1alpha1(), machineEventRecorder, *namedMachinesPath)
	// if err != nil {
	// 	glog.Fatalf("Could not create vSphere machine actuator: %v", err)
	// }

	machineActuator, err := vsphere.NewGovmomiMachineActuator(client.ClusterV1alpha1(), machineClientSet, si.Factory.Cluster().V1alpha1(), machineEventRecorder)
	if err != nil {
		glog.Fatalf("Could not create vSphere machine actuator: %v", err)
	}

	clusterEventRecorder, err := createRecorder(clusterClientSet, "cluster-controller-manager")
	if err != nil {
		glog.Fatalf("Could not create vSphere event recorder: %v", err)
	}

	clusterActuator, err := vsphere.NewClusterActuator(client.ClusterV1alpha1(), clusterClientSet, si.Factory.Cluster().V1alpha1(), clusterEventRecorder)
	if err != nil {
		glog.Fatalf("Could not create vSphere cluster actuator: %v", err)
	}

	controllers := []controller.Controller{
		machine.NewMachineController(config, si, machineActuator),
		cluster.NewClusterController(config, si, clusterActuator),
	}
	controller.StartControllerManager(controllers...)

	// Blockforever
	select {}
}

func createRecorder(kubeClient *kubernetes.Clientset, source string) (record.EventRecorder, error) {

	eventsScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(eventsScheme); err != nil {
		return nil, err
	}
	// We also emit events for our own types.
	clusterapiclientsetscheme.AddToScheme(eventsScheme)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})
	return eventBroadcaster.NewRecorder(eventsScheme, corev1.EventSource{Component: source}), nil
}
