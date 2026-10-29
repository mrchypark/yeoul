package ci

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

type deployment struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		Replicas int `yaml:"replicas"`
		Strategy struct {
			Type string `yaml:"type"`
		} `yaml:"strategy"`
		Template struct {
			Spec struct {
				AutomountServiceAccountToken *bool               `yaml:"automountServiceAccountToken"`
				SecurityContext              podSecurityContext  `yaml:"securityContext"`
				InitContainers               []manifestContainer `yaml:"initContainers"`
				Containers                   []manifestContainer `yaml:"containers"`
				Volumes                      []struct {
					Name                  string `yaml:"name"`
					PersistentVolumeClaim *struct {
						ClaimName string `yaml:"claimName"`
					} `yaml:"persistentVolumeClaim"`
					EmptyDir *struct {
						Medium    string `yaml:"medium"`
						SizeLimit string `yaml:"sizeLimit"`
					} `yaml:"emptyDir"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type podSecurityContext struct {
	RunAsNonRoot   bool `yaml:"runAsNonRoot"`
	RunAsUser      int  `yaml:"runAsUser"`
	RunAsGroup     int  `yaml:"runAsGroup"`
	FSGroup        int  `yaml:"fsGroup"`
	SeccompProfile struct {
		Type string `yaml:"type"`
	} `yaml:"seccompProfile"`
}

type manifestContainer struct {
	Name         string   `yaml:"name"`
	Image        string   `yaml:"image"`
	Command      []string `yaml:"command"`
	Args         []string `yaml:"args"`
	VolumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
		ReadOnly  bool   `yaml:"readOnly"`
	} `yaml:"volumeMounts"`
	SecurityContext struct {
		AllowPrivilegeEscalation *bool `yaml:"allowPrivilegeEscalation"`
		ReadOnlyRootFilesystem   bool  `yaml:"readOnlyRootFilesystem"`
		Capabilities             struct {
			Drop []string `yaml:"drop"`
		} `yaml:"capabilities"`
	} `yaml:"securityContext"`
}

func TestSingleNodeKubernetesManifestContract(t *testing.T) {
	path := os.Getenv("YEOUL_K8S_MANIFEST")
	if path == "" {
		path = filepath.Join("..", "..", "deploy", "kubernetes", "deployment.yaml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var manifest deployment
	found := false
	for {
		var candidate deployment
		err := decoder.Decode(&candidate)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse Kubernetes manifest: %v", err)
		}
		if candidate.Kind == "PersistentVolumeClaim" {
			t.Fatal("no-PVC deployment must not render a PersistentVolumeClaim resource")
		}
		if candidate.Kind == "Deployment" {
			if found {
				t.Fatal("rendered manifest contains more than one Deployment")
			}
			manifest = candidate
			found = true
		}
	}
	if !found {
		t.Fatal("manifest does not contain a Deployment")
	}
	if manifest.APIVersion != "apps/v1" || manifest.Kind != "Deployment" {
		t.Fatalf("manifest identity = %s %s", manifest.APIVersion, manifest.Kind)
	}
	if manifest.Spec.Replicas != 1 || manifest.Spec.Strategy.Type != "Recreate" {
		t.Fatalf("single-writer policy = replicas %d, strategy %q", manifest.Spec.Replicas, manifest.Spec.Strategy.Type)
	}
	pod := manifest.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("pod must disable automatic service-account token mounting")
	}
	if !pod.SecurityContext.RunAsNonRoot || pod.SecurityContext.RunAsUser != 65532 ||
		pod.SecurityContext.RunAsGroup != 65532 || pod.SecurityContext.FSGroup != 65532 ||
		pod.SecurityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Fatalf("pod security context must require non-root and RuntimeDefault seccomp: %+v", pod.SecurityContext)
	}
	if len(pod.InitContainers) != 1 || len(pod.Containers) != 1 {
		t.Fatalf("expected one preflight init container and one daemon, got %d and %d", len(pod.InitContainers), len(pod.Containers))
	}
	preflight, daemon := pod.InitContainers[0], pod.Containers[0]
	if preflight.Name != "restore" || daemon.Name != "yeould" || preflight.Image == "" || preflight.Image != daemon.Image {
		t.Fatalf("restore/main image contract mismatch: init=%+v daemon=%+v", preflight, daemon)
	}
	if !reflect.DeepEqual(preflight.Command, []string{"/yeould"}) ||
		!hasArgs(preflight.Args,
			"--db", "/data/yeoul.ltdb",
			"--rhiza-backup-config", "/tmp/config/backup.json",
			"--restore-if-missing", "--restore-only") ||
		hasArg(preflight.Args, "--create") || hasArg(preflight.Args, "--listen") {
		t.Fatalf("init container must restore and close the database without serving HTTP: %+v", preflight.Args)
	}
	if !hasArgs(daemon.Args, "--db", "/data/yeoul.ltdb") ||
		!hasArgs(daemon.Args, "--rhiza-backup-config", "/tmp/config/backup.json") ||
		hasArg(daemon.Args, "--restore-if-missing") || hasArg(daemon.Args, "--restore-only") || hasArg(daemon.Args, "--create") {
		t.Fatalf("main daemon must open the restored database without restoring or creating it: %+v", daemon.Args)
	}
	if !hasMount(preflight.VolumeMounts, "data", "/data", false) ||
		!hasMount(daemon.VolumeMounts, "data", "/data", false) ||
		!hasMount(preflight.VolumeMounts, "rhiza-backup-config", "/tmp/config", true) ||
		!hasMount(daemon.VolumeMounts, "rhiza-backup-config", "/tmp/config", true) ||
		!hasMount(daemon.VolumeMounts, "tmp", "/tmp", false) {
		t.Fatal("both containers must share emptyDir data and read-only config mounts, and daemon must retain its /tmp mount")
	}
	if !containerSecurityHardened(preflight) || !containerSecurityHardened(daemon) {
		t.Fatal("init and daemon containers must disable privilege escalation, use a read-only root filesystem, and drop all capabilities")
	}
	dataUsesEmptyDir := false
	for _, volume := range pod.Volumes {
		if volume.PersistentVolumeClaim != nil {
			t.Fatal("no-PVC deployment must not reference a persistentVolumeClaim volume")
		}
		if volume.Name == "data" && volume.EmptyDir != nil {
			dataUsesEmptyDir = true
		}
	}
	if !dataUsesEmptyDir {
		t.Fatal("data volume must use emptyDir")
	}
}

func containerSecurityHardened(container manifestContainer) bool {
	return container.SecurityContext.AllowPrivilegeEscalation != nil &&
		!*container.SecurityContext.AllowPrivilegeEscalation &&
		container.SecurityContext.ReadOnlyRootFilesystem &&
		reflect.DeepEqual(container.SecurityContext.Capabilities.Drop, []string{"ALL"})
}

func hasArgs(args []string, expected ...string) bool {
	for i := 0; i+len(expected) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(expected)], expected) {
			return true
		}
	}
	return false
}

func hasArg(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

func hasMount(mounts []struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly"`
}, name, path string, readOnly bool) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == path && mount.ReadOnly == readOnly {
			return true
		}
	}
	return false
}
