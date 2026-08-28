package assets

const ManifestFile = "orbit-assets.yaml"

type Object struct {
	Path        string `yaml:"path"`
	SHA256      string `yaml:"sha256"`
	Size        int64  `yaml:"size"`
	ContentType string `yaml:"content_type,omitempty"`
}

type Manifest struct {
	Version int      `yaml:"version"`
	Objects []Object `yaml:"objects"`
}

func (m *Manifest) Find(path string) (Object, bool) {
	if m == nil {
		return Object{}, false
	}
	for _, o := range m.Objects {
		if o.Path == path {
			return o, true
		}
	}
	return Object{}, false
}

func (m *Manifest) Upsert(obj Object) {
	if m.Version == 0 {
		m.Version = 1
	}
	for i, o := range m.Objects {
		if o.Path == obj.Path {
			m.Objects[i] = obj
			return
		}
	}
	m.Objects = append(m.Objects, obj)
}
