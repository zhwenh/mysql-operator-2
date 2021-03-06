/*
Copyright 2018 Pressinfra SRL

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

package mysqlcluster

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	api "github.com/presslabs/mysql-operator/pkg/apis/mysql/v1alpha1"
	ticlientset "github.com/presslabs/mysql-operator/pkg/generated/clientset/versioned"
	"github.com/presslabs/mysql-operator/pkg/util/options"
	orc "github.com/presslabs/mysql-operator/pkg/util/orchestrator"
)

// Interface is for cluster Factory
type Interface interface {
	// Sync is the method that tries to sync the cluster.
	Sync(ctx context.Context) error
}

// cluster factory
type cFactory struct {
	cluster *api.MysqlCluster
	opt     *options.Options

	namespace string

	client   kubernetes.Interface
	myClient ticlientset.Interface
	rec      record.EventRecorder

	configHash string
	secretHash string
}

// New creates a new cluster factory
func New(cluster *api.MysqlCluster, opt *options.Options, klient kubernetes.Interface,
	myClient ticlientset.Interface, ns string, rec record.EventRecorder) Interface {
	return &cFactory{
		cluster:    cluster,
		opt:        opt,
		client:     klient,
		myClient:   myClient,
		namespace:  ns,
		rec:        rec,
		configHash: "1",
		secretHash: "1",
	}
}

const (
	statusUpToDate = "up-to-date"
	statusCreated  = "created"
	statusUpdated  = "updated"
	statusFailed   = "faild"
	statusOk       = "ok"
	statusSkip     = "skip"
)

type component struct {
	// the name that will be showed in logs
	alias  string
	name   string
	syncFn func() (string, error)
	//event reason when sync faild
	reasonFailed string
	// event reason when object is updated
	reasonUpdated string
}

func (f *cFactory) getComponents() []component {
	return []component{
		component{
			alias:         "cluster-secret",
			name:          f.cluster.Spec.SecretName,
			syncFn:        f.syncClusterSecret,
			reasonFailed:  api.EventReasonDbSecretFailed,
			reasonUpdated: api.EventReasonDbSecretUpdated,
		},
		component{
			alias:         "config-map",
			name:          f.cluster.GetNameForResource(api.ConfigMap),
			syncFn:        f.syncConfigMysqlMap,
			reasonFailed:  api.EventReasonConfigMapFailed,
			reasonUpdated: api.EventReasonConfigMapUpdated,
		},
		component{
			alias:         "headless-service",
			name:          f.cluster.GetNameForResource(api.HeadlessSVC),
			syncFn:        f.syncHeadlessService,
			reasonFailed:  api.EventReasonServiceFailed,
			reasonUpdated: api.EventReasonServiceUpdated,
		},
		component{
			alias:         "statefulset",
			name:          f.cluster.GetNameForResource(api.StatefulSet),
			syncFn:        f.syncStatefulSet,
			reasonFailed:  api.EventReasonSFSFailed,
			reasonUpdated: api.EventReasonSFSUpdated,
		},
		component{
			alias:         "backup-cron-job",
			name:          f.cluster.GetNameForResource(api.BackupCronJob),
			syncFn:        f.syncBackupCronJob,
			reasonFailed:  api.EventReasonCronJobFailed,
			reasonUpdated: api.EventReasonCronJobUpdated,
		},
	}
}

func (f *cFactory) Sync(ctx context.Context) error {
	for _, comp := range f.getComponents() {
		state, err := comp.syncFn()
		if err != nil {
			glog.Warningf("[%s]: failed syncing %s: ", comp.alias, comp.name, err.Error())
			err = fmt.Errorf("%s sync failed: %s", comp.name, err)
			f.rec.Event(f.cluster, api.EventWarning, comp.reasonFailed, err.Error())
			return err
		} else {
			glog.V(2).Infof("[%s]: %s ... (%s)", comp.alias, comp.name, state)
		}
		switch state {
		case statusCreated, statusUpdated:
			f.rec.Event(f.cluster, api.EventNormal, comp.reasonUpdated, "")
		}
	}

	// Register nodes in orchestrator
	if len(f.cluster.Spec.GetOrcUri()) != 0 {
		// try to discover ready nodes into orchestrator
		client := orc.NewFromUri(f.cluster.Spec.GetOrcUri())
		for i := 0; i < int(f.cluster.Status.ReadyNodes); i++ {
			host := f.getHostForReplica(i)
			if err := client.Discover(host, MysqlPort); err != nil {
				glog.Warningf("Failed to register %s with orchestrator: %s", host, err.Error())
			}
		}
	}
	return nil
}

func (f *cFactory) getOwnerReferences(ors ...[]metav1.OwnerReference) []metav1.OwnerReference {
	rs := []metav1.OwnerReference{
		f.cluster.AsOwnerReference(),
	}
	for _, or := range ors {
		for _, o := range or {
			rs = append(rs, o)
		}
	}
	return rs
}

func (f *cFactory) getHostForReplica(no int) string {
	return fmt.Sprintf("%s-%d.%s.%s", f.cluster.GetNameForResource(api.StatefulSet), no,
		f.cluster.GetNameForResource(api.HeadlessSVC),
		f.cluster.Namespace)
}
