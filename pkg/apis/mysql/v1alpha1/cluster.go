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

package v1alpha1

import (
	"fmt"
	"strconv"

	"github.com/golang/glog"
	apiv1 "k8s.io/api/core/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/presslabs/mysql-operator/pkg/util/options"
	orc "github.com/presslabs/mysql-operator/pkg/util/orchestrator"
)

const (
	innodbBufferSizePercent = 80
)

const (
	_        = iota // ignore first value by assigning to blank identifier
	KB int64 = 1 << (10 * iota)
	MB
	GB
)

var (
	opt *options.Options
)

func init() {
	opt = options.GetOptions()
}

// AsOwnerReference returns the MysqlCluster owner references.
func (c *MysqlCluster) AsOwnerReference() metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.String(),
		Kind:       MysqlClusterKind,
		Name:       c.Name,
		UID:        c.UID,
		Controller: &trueVar,
	}
}

// UpdateDefaults sets the defaults for Spec and Status
func (c *MysqlCluster) UpdateDefaults(opt *options.Options) error {
	return c.Spec.UpdateDefaults(opt, c)
}

// UpdateDefaults updates Spec defaults
func (c *ClusterSpec) UpdateDefaults(opt *options.Options, cluster *MysqlCluster) error {
	if len(c.MysqlVersion) == 0 {
		c.MysqlVersion = opt.MysqlImageTag
	}

	if err := c.PodSpec.UpdateDefaults(opt, cluster); err != nil {
		return err
	}

	if len(c.MysqlConf) == 0 {
		c.MysqlConf = make(MysqlConf)
	}

	// configure mysql based on:
	// https://www.percona.com/blog/2018/03/26/mysql-8-0-innodb_dedicated_server-variable-optimizes-innodb/

	// set innodb-buffer-pool-size if not set
	if _, ok := c.MysqlConf["innodb-buffer-pool-size"]; !ok {
		if mem := c.PodSpec.Resources.Requests.Memory(); mem != nil {
			var bufferSize int64
			if mem.Value() < GB {
				// RAM < 1G => buffer size set to 128M
				bufferSize = 128 * MB
			} else if mem.Value() <= 4*GB {
				// RAM <= 4GB => buffer size set to RAM * 0.5
				bufferSize = int64(float64(mem.Value()) * 0.5)
			} else {
				// RAM > 4GB => buffer size set to RAM * 0.75
				bufferSize = int64(float64(mem.Value()) * 0.75)
			}

			c.MysqlConf["innodb-buffer-pool-size"] = strconv.FormatInt(bufferSize, 10)
		}
	}

	if _, ok := c.MysqlConf["innodb-log-file-size"]; !ok {
		if mem := c.PodSpec.Resources.Requests.Memory(); mem != nil {
			var logFileSize int64
			if mem.Value() < GB {
				// RAM < 1G
				logFileSize = 48 * MB
			} else if mem.Value() <= 4*GB {
				// RAM <= 4GB
				logFileSize = 128 * MB
			} else if mem.Value() <= 8*GB {
				// RAM <= 8GB
				logFileSize = 512 * GB
			} else if mem.Value() <= 16*GB {
				// RAM <= 16GB
				logFileSize = 1 * GB
			} else {
				// RAM > 16GB
				logFileSize = 2 * GB
			}

			c.MysqlConf["innodb-log-file-size"] = strconv.FormatInt(logFileSize, 10)
		}
	}

	return c.VolumeSpec.UpdateDefaults()
}

// GetHelperImage return helper image from options
func (c *ClusterSpec) GetHelperImage() string {
	return opt.HelperImage
}

// GetMetricsExporterImage return helper image from options
func (c *ClusterSpec) GetMetricsExporterImage() string {
	return opt.MetricsExporterImage
}

// GetOrcUri return the orchestrator uri
func (c *ClusterSpec) GetOrcUri() string {
	return opt.OrchestratorUri
}

// GetMysqlImage returns mysql image, composed from oprions and  Spec.MysqlVersion
func (c *ClusterSpec) GetMysqlImage() string {
	return opt.MysqlImage + ":" + c.MysqlVersion
}

const (
	resourceRequestCPU    = "200m"
	resourceRequestMemory = "1Gi"

	resourceStorage = "1Gi"
)

// UpdateDefaults for PodSpec
func (ps *PodSpec) UpdateDefaults(opt *options.Options, cluster *MysqlCluster) error {
	if len(ps.ImagePullPolicy) == 0 {
		ps.ImagePullPolicy = opt.ImagePullPolicy
	}

	if len(ps.Resources.Requests) == 0 {
		ps.Resources = apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse(resourceRequestCPU),
				apiv1.ResourceMemory: resource.MustParse(resourceRequestMemory),
			},
		}
	}

	// set pod antiaffinity to nodes stay away from other nodes.
	if ps.Affinity.PodAntiAffinity == nil {
		ps.Affinity.PodAntiAffinity = &core.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []core.WeightedPodAffinityTerm{
				core.WeightedPodAffinityTerm{
					Weight: 100,
					PodAffinityTerm: core.PodAffinityTerm{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: cluster.GetLabels(),
						},
					},
				},
			},
		}
	}
	return nil
}

// UpdateDefaults for VolumeSpec
func (vs *VolumeSpec) UpdateDefaults() error {
	if len(vs.AccessModes) == 0 {
		vs.AccessModes = []apiv1.PersistentVolumeAccessMode{
			apiv1.ReadWriteOnce,
		}
	}

	if len(vs.Resources.Requests) == 0 {
		vs.Resources = apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceStorage: resource.MustParse(resourceStorage),
			},
		}
	}

	return nil
}

// ResourceName is the type for aliasing resources that will be created.
type ResourceName string

const (
	// HeadlessSVC is the alias of the headless service resource
	HeadlessSVC ResourceName = "headless"
	// StatefulSet is the alias of the statefulset resource
	StatefulSet ResourceName = "mysql"
	// ConfigMap is the alias for mysql configs, the config map resource
	ConfigMap ResourceName = "config-files"
	// BackupCronJob is the name of cron job
	BackupCronJob ResourceName = "backup-cron"
)

func (c *MysqlCluster) GetNameForResource(name ResourceName) string {
	switch name {
	case HeadlessSVC, StatefulSet, ConfigMap, BackupCronJob:
		return GetNameForResource(name, c.Name)
	default:
		return GetNameForResource(name, c.Name)
	}
}

func GetNameForResource(name ResourceName, clusterName string) string {
	return fmt.Sprintf("%s-mysql", clusterName)
}

func (c *MysqlCluster) GetHealtySlaveHost() string {
	if c.Status.ReadyNodes < 1 {
		glog.Warning("[GetHealtySlaveHost]: no ready nodes yet!")
		glog.V(2).Infof("[GetHealtySlaveHost]: The slave host is: %s", c.GetPodHostName(0))
		return c.GetPodHostName(0)
	}
	host := c.GetPodHostName(c.Status.ReadyNodes - 1)

	if len(c.Spec.GetOrcUri()) != 0 {
		glog.V(2).Info("[GetHealtySlaveHost]: Use orchestrator to get slave host.")
		client := orc.NewFromUri(c.Spec.GetOrcUri())
		replicas, err := client.ClusterOSCReplicas(c.Name)
		if err != nil {
			glog.Errorf("[GetHealtySlaveHost] orc failed with: %s", err)
			return host
		}
		for _, r := range replicas {
			if r.SecondsBehindMaster.Valid && r.SecondsBehindMaster.Int64 <= 5 {
				glog.V(2).Infof("[GetHealtySlaveHost]: Using orc we choses: %s",
					r.Key.Hostname)
				host = r.Key.Hostname
			}
		}
	}

	glog.V(2).Infof("[GetHealtySlaveHost]: The slave host is: %s", host)
	return host
}

func (c *MysqlCluster) GetMasterHost() string {
	masterHost := c.GetPodHostName(0)
	// connect to orc and get the master host of the cluster.
	if len(c.Spec.GetOrcUri()) != 0 {
		client := orc.NewFromUri(c.Spec.GetOrcUri())
		orcClusterName := fmt.Sprintf("%s.%s", c.Name, c.Namespace)
		if inst, err := client.Master(orcClusterName); err == nil {
			masterHost = inst.Key.Hostname
		} else {
			glog.Warningf(
				"Failed getting master for %s: %s, falling back to default.",
				orcClusterName, err,
			)
		}
	}

	return masterHost
}

func (c *MysqlCluster) GetPodHostName(p int) string {
	pod := fmt.Sprintf("%s-%d", c.GetNameForResource(StatefulSet), p)
	return fmt.Sprintf("%s.%s", pod, c.GetNameForResource(HeadlessSVC))
}

func (c *MysqlCluster) GetLabels() map[string]string {
	return map[string]string{
		"app":           "mysql-operator",
		"mysql_cluster": c.Name,
	}
}
