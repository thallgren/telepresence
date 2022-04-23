package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	"github.com/telepresenceio/telepresence/v2/pkg/agentmap"
	"github.com/telepresenceio/telepresence/v2/pkg/client"
	"github.com/telepresenceio/telepresence/v2/pkg/client/userd/k8s"
	"github.com/telepresenceio/telepresence/v2/pkg/install"
	"github.com/telepresenceio/telepresence/v2/pkg/k8sapi"
	"github.com/telepresenceio/telepresence/v2/pkg/version"
)

type genYAMLInfo struct {
	outputFile string
	inputFile  string
}

func genYAMLCommand() *cobra.Command {
	info := genYAMLInfo{}
	cmd := &cobra.Command{
		Use:  "genyaml",
		Args: cobra.NoArgs,

		Short: "Generate YAML for use in kubernetes manifests.",
		Long: `Generate traffic-agent yaml for use in kubernetes manifests.
This allows the traffic agent to be injected by hand into existing kubernetes manifests.
For your modified workload to be valid, you'll have to manually inject both the container and the volume; you can do this by running "genyaml container" or "genyaml volume"
It is recommended that you not do this unless strictly necessary. Instead, we suggest use of the webhook injector to configure traffic agents.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("please run genyaml as \"genyaml container\" or \"genyaml volume\"")
		},
	}
	flags := cmd.PersistentFlags()
	flags.StringVar(&info.inputFile, "input", "",
		"Path to the yaml containing the workload definition (i.e. Deployment, StatefulSet, etc). Pass '-' for stdin.")
	flags.StringVar(&info.outputFile, "output", "-",
		"Path to the file to place the output in. Defaults to '-' which means stdout.")
	_ = cmd.MarkPersistentFlagRequired("input")
	cmd.AddCommand(
		genContainerSubCommand(&info),
		genAgentConfigMapSubCommand(&info),
		genVolumeSubCommand(&info),
	)
	return cmd
}

func getInputReader(inputFile string) (io.ReadCloser, error) {
	if inputFile == "-" {
		return os.Stdin, nil
	}
	f, err := os.Open(inputFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open input file %s: %w", inputFile, err)
	}
	return f, nil
}

func (i *genYAMLInfo) getOutputWriter() (io.WriteCloser, error) {
	if i.outputFile == "-" {
		return os.Stdout, nil
	}
	f, err := os.Create(i.outputFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open output file %s: %w", i.outputFile, err)
	}
	return f, nil
}

func (i *genYAMLInfo) parseWorkload(ctx context.Context) (k8sapi.Workload, error) {
	f, err := getInputReader(i.inputFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("error reading from %s: %w", i.inputFile, err)
	}

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schema.GroupVersion{Group: apps.GroupName, Version: "v1"}, &apps.StatefulSet{}, &apps.Deployment{}, &apps.ReplicaSet{})
	codecFactory := serializer.NewCodecFactory(scheme)
	deserializer := codecFactory.UniversalDeserializer()

	obj, kind, err := deserializer.Decode(b, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to parse yaml in %s: %w", i.inputFile, err)
	}
	wl, err := k8sapi.WrapWorkload(obj)
	if err != nil {
		return nil, fmt.Errorf("unexpected object of kind %s; please pass in a Deployment, ReplicaSet, or StatefulSet", kind)
	}
	return wl, nil
}

func (i *genYAMLInfo) writeObjToOutput(obj interface{}) error {
	// We use sigs.ks8.io/yaml because it treats json serialization tags as if they were yaml tags.
	doc, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("unable to marshal agent container: %w", err)
	}
	w, err := i.getOutputWriter()
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.Write(doc)
	if err != nil {
		return fmt.Errorf("unable to write to output %s: %w", i.outputFile, err)
	}
	return nil
}

type genAgentConfigMap struct {
	*genYAMLInfo
}

func genAgentConfigMapSubCommand(yamlInfo *genYAMLInfo) *cobra.Command {
	info := genAgentConfigMap{genYAMLInfo: yamlInfo}
	cmd := &cobra.Command{
		Use:   "configmap",
		Args:  cobra.NoArgs,
		Short: "Generate YAML for the traffic-agent configmap entry.",
		Long:  "Generate YAML for the traffic-agent configmap entry. See genyaml for more info on what this means",
		RunE: func(cmd *cobra.Command, args []string) error {
			return info.run(cmd, kubeFlagMap(kubeFlags))
		},
	}
	cmd.Flags().AddFlagSet(kubeFlags)
	return cmd
}

func withK8sInterface(ctx context.Context, kubeFlags map[string]string) (context.Context, error) {
	c, err := k8s.NewConfig(ctx, kubeFlags)
	if err == nil {
		if rc, err := c.ConfigFlags.ToRESTConfig(); err == nil {
			if cs, err := kubernetes.NewForConfig(rc); err == nil {
				ctx = k8sapi.WithK8sInterface(ctx, cs)
			}
		}
	}
	return ctx, err
}

func (g *genAgentConfigMap) run(cmd *cobra.Command, kubeFlags map[string]string) error {
	ctx, err := withK8sInterface(cmd.Context(), kubeFlags)
	if err != nil {
		return fmt.Errorf("unable to initialize k8s api: %w", err)
	}

	wl, err := g.parseWorkload(ctx)
	if err != nil {
		return err
	}
	am, err := agentmap.Generate(ctx, wl, &agentmap.GeneratorConfig{
		AgentPort:             9900,
		APIPort:               0,
		AgentRegistryAndImage: "docker.io/datawire/tel2:" + strings.TrimPrefix(client.Version(), "v"),
		ManagerNamespace:      "ambassador",
	})
	if err != nil {
		return err
	}
	return g.writeObjToOutput(am)
}

type genContainerInfo struct {
	*genYAMLInfo
	containerName string
	serviceName   string
	port          int
	proto         string
	agentPort     int
	appProto      string
}

func genContainerSubCommand(yamlInfo *genYAMLInfo) *cobra.Command {
	kubeFlags := pflag.NewFlagSet("Kubernetes flags", 0)
	info := genContainerInfo{genYAMLInfo: yamlInfo}
	cmd := &cobra.Command{
		Use:   "container",
		Args:  cobra.NoArgs,
		Short: "Generate YAML for the traffic-agent container.",
		Long:  "Generate YAML for the traffic-agent container. See genyaml for more info on what this means",
		RunE: func(cmd *cobra.Command, args []string) error {
			return info.run(cmd, kubeFlagMap(kubeFlags))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&info.containerName, "container-name", "",
		"The name of the container hosting the application you wish to intercept.")
	flags.IntVar(&info.port, "port", 0,
		"The port number you wish to intercept")
	flags.StringVar(&info.proto, "protocol", string(core.ProtocolTCP),
		`The transport protocol the port speaks, i.e. "tcp" or "udp"`)
	flags.StringVar(&info.appProto, "app-protocol", "",
		`The application protocol the port speaks, i.e. "http", "grpc", "https", ...`)
	flags.IntVar(&info.agentPort, "agent-port", 9900,
		"The port number you wish the agent to listen on.")
	flags.StringVar(&info.serviceName, "service-name", "",
		`The name of the service that's exposing this deployment and that you will wish to intercept.
Defaults to the name of the deployment.`)
	_ = cmd.MarkFlagRequired("container-name")
	_ = cmd.MarkFlagRequired("port")

	kubeConfig := genericclioptions.NewConfigFlags(false)
	kubeConfig.Namespace = nil // "connect", don't take --namespace
	kubeConfig.AddFlags(kubeFlags)
	flags.AddFlagSet(kubeFlags)
	return cmd
}

func (i *genContainerInfo) run(cmd *cobra.Command, kubeFlags map[string]string) error {
	ctx := cmd.Context()

	wl, err := i.parseWorkload(ctx)
	if err != nil {
		return err
	}

	containers := wl.GetPodTemplate().Spec.Containers
	containerIdx := -1
	for j, c := range containers {
		if c.Name == i.containerName {
			containerIdx = j
			break
		}
	}
	if containerIdx < 0 {
		return fmt.Errorf("container %s not found in %s given", i.containerName, wl.GetKind())
	}
	container := &containers[containerIdx]

	if i.serviceName == "" {
		i.serviceName = wl.GetName()
	}

	cfg := client.GetConfig(ctx)
	k8sConfig, err := k8s.NewConfig(ctx, kubeFlags)
	if err != nil {
		return fmt.Errorf("unable to get k8s config: %w", err)
	}

	registry := cfg.Images.Registry(ctx)
	agentImage := cfg.Images.AgentImage(ctx)
	// Use sane defaults if the user hasn't configured the registry and/or image
	if registry == "" {
		registry = "datawire"
	}
	if agentImage == "" {
		agentImage = "tel2:" + strings.TrimPrefix(version.Version, "v")
	}
	agentContainer := install.AgentContainer(
		i.serviceName,
		fmt.Sprintf("%s/%s", registry, agentImage),
		container,
		core.ContainerPort{
			Protocol:      core.Protocol(i.proto),
			ContainerPort: int32(i.agentPort),
		},
		i.port,
		i.appProto,
		cfg.TelepresenceAPI.Port,
		k8sConfig.GetManagerNamespace(),
	)

	return i.writeObjToOutput(&agentContainer)
}

type genVolumeInfo struct {
	*genYAMLInfo
}

func genVolumeSubCommand(yamlInfo *genYAMLInfo) *cobra.Command {
	info := genVolumeInfo{genYAMLInfo: yamlInfo}
	cmd := &cobra.Command{
		Use:   "volume",
		Args:  cobra.NoArgs,
		Short: "Generate YAML for the traffic-agent volume.",
		Long:  "Generate YAML for the traffic-agent volume. See genyaml for more info on what this means",
		RunE:  info.run,
	}
	return cmd
}

func (i *genVolumeInfo) run(_ *cobra.Command, _ []string) error {
	volume := install.AgentVolume()
	return i.writeObjToOutput(&volume)
}
