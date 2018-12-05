package tiltfile2

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/docker/distribution/reference"
	"github.com/google/skylark"

	"github.com/windmilleng/tilt/internal/k8s"
	"github.com/windmilleng/tilt/internal/logger"
	"github.com/windmilleng/tilt/internal/model"
)

type tiltfileState struct {
	// set at creation
	ctx      context.Context
	filename string

	// added to during execution
	configFiles    []string
	images         []*dockerImage
	imagesByName   map[string]*dockerImage
	k8s            []*k8sResource
	k8sByName      map[string]*k8sResource
	k8sUnresourced []k8s.K8sEntity
}

func newTiltfileState(ctx context.Context, filename string) *tiltfileState {
	return &tiltfileState{
		ctx:          ctx,
		filename:     filename,
		imagesByName: make(map[string]*dockerImage),
		k8sByName:    make(map[string]*k8sResource),
		configFiles:  []string{filename},
	}
}

func (s *tiltfileState) exec() error {
	thread := &skylark.Thread{
		Print: func(_ *skylark.Thread, msg string) {
			logger.Get(s.ctx).Infof("%s", msg)
		},
	}
	_, err := skylark.ExecFile(thread, s.filename, nil, s.builtins())
	return err
}

// Builtin functions

const (
	// build functions
	dockerBuildN = "docker_build"
	fastBuildN   = "fast_build"

	// k8s functions
	k8sYamlN     = "k8s_yaml"
	k8sResourceN = "k8s_resource"
	portForwardN = "port_forward"

	// file functions
	localGitRepoN = "local_git_repo"
	localN        = "local"
	readFileN     = "read_file"
	kustomizeN    = "kustomize"
)

func (s *tiltfileState) builtins() skylark.StringDict {
	r := make(skylark.StringDict)
	add := func(name string, fn func(thread *skylark.Thread, fn *skylark.Builtin, args skylark.Tuple, kwargs []skylark.Tuple) (skylark.Value, error)) {
		r[name] = skylark.NewBuiltin(name, fn)
	}

	add(dockerBuildN, s.dockerBuild)
	add(fastBuildN, s.fastBuild)

	add(k8sYamlN, s.k8sYaml)
	add(k8sResourceN, s.k8sResource)
	add(portForwardN, s.portForward)

	add(localGitRepoN, s.localGitRepo)
	add(localN, s.local)
	add(readFileN, s.skylarkReadFile)
	add(kustomizeN, s.kustomize)
	return r
}

const unresourcedName = "unresourced"

func (s *tiltfileState) assemble() (result []*k8sResource, outerErr error) {
	if len(s.k8sUnresourced) > 0 {
		logger.Get(s.ctx).Infof("deferring")
		defer func() {
			logger.Get(s.ctx).Infof("called %v", outerErr)
			if outerErr != nil {
				return
			}
			logger.Get(s.ctx).Infof("adding unresourced")
			// At the end, add everything that's left
			r, innerErr := s.makeK8sResource(unresourcedName)
			outerErr = innerErr
			if outerErr != nil {
				return
			}
			r.k8s = s.k8sUnresourced
			result = s.k8s
		}()
	}
	images, err := s.findUnresourcedImages()
	if err != nil {
		return nil, err
	}
	for _, image := range images {
		target, err := s.findExpandTarget(image)
		if err != nil {
			return nil, err
		}
		if err := s.extractImage(target, image); err != nil {
			return nil, err
		}
	}

	return s.k8s, nil
}

func (s *tiltfileState) findExpandTarget(image reference.Named) (*k8sResource, error) {
	// first, match an empty resource that has this exact imageRef
	for _, r := range s.k8s {
		if len(r.k8s) == 0 && r.imageRef == image.Name() {
			return r, nil
		}
	}

	// next, match an empty resource that has the same name
	name := filepath.Base(image.Name())
	for _, r := range s.k8s {
		if len(r.k8s) == 0 && r.name == name {
			return r, nil
		}
	}

	// otherwise, create a new resource
	return s.makeK8sResource(name)
}

func (s *tiltfileState) findUnresourcedImages() ([]reference.Named, error) {
	var result []reference.Named

	for _, e := range s.k8sUnresourced {
		images, err := e.FindImages()
		if err != nil {
			return nil, err
		}
		var entityImages []reference.Named
		for _, image := range images {
			if _, ok := s.imagesByName[image.Name()]; ok {
				entityImages = append(entityImages, image)
			}
		}
		if len(entityImages) == 0 {
			continue
		}
		if len(entityImages) > 1 {
			str, err := k8s.SerializeYAML([]k8s.K8sEntity{e})
			if err != nil {
				str = err.Error()
			}
			return nil, fmt.Errorf("Found an entity with multiple images registered with k8s_yaml. Tilt doesn't support this yet; please reach out so we can understand and prioritize this case. found images: %q, entity: %q.", entityImages, str)
		}
		result = append(result, entityImages[0])
	}
	return result, nil
}

func (s *tiltfileState) extractImage(dest *k8sResource, imageRef reference.Named) error {
	extracted, remaining, err := k8s.FilterByImage(s.k8sUnresourced, imageRef)
	if err != nil {
		return err
	}

	dest.k8s = append(dest.k8s, extracted...)
	s.k8sUnresourced = remaining

	for _, e := range extracted {
		podTemplates, err := k8s.ExtractPodTemplateSpec(e)
		if err != nil {
			return err
		}
		for _, template := range podTemplates {
			extracted, remaining, err := k8s.FilterByLabels(s.k8sUnresourced, template.Labels)
			if err != nil {
				return err
			}
			dest.k8s = append(dest.k8s, extracted...)
			s.k8sUnresourced = remaining
		}
	}
	dest.imageRef = imageRef.Name()
	return nil
}

func (s *tiltfileState) translate(resources []*k8sResource) ([]model.Manifest, error) {
	var result []model.Manifest
	for _, r := range resources {
		m := model.Manifest{
			Name: model.ManifestName(r.name),
		}

		k8sYaml, err := k8s.SerializeYAML(r.k8s)
		if err != nil {
			return nil, err
		}

		m = m.WithPortForwards(s.portForwardsToDomain(r)). // FIXME(dbentley)
									WithK8sYAML(k8sYaml)

		if r.imageRef != "" {
			image := s.imagesByName[r.imageRef]
			m.Mounts = s.mountsToDomain(image)
			m.Entrypoint = model.ToShellCmd(image.entrypoint)
			m.BaseDockerfile = image.baseDockerfile.String()
			m.Steps = image.steps
			m.StaticDockerfile = image.staticDockerfile.String()
			m.StaticBuildPath = string(image.staticBuildPath.path)
			m.StaticBuildArgs = image.staticBuildArgs
			m.Repos = s.reposToDomain(image)
			m = m.WithDockerRef(image.ref).
				WithTiltFilename(image.tiltFilename).
				WithCachePaths(image.cachePaths)
		}
		result = append(result, m)
	}

	return result, nil
}

func badTypeErr(b *skylark.Builtin, ex interface{}, v skylark.Value) error {
	return fmt.Errorf("%v expects a %T; got %T (%v)", b.Name(), ex, v, v)
}