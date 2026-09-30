package setup

import (
	"errors"
	"reflect"
	"testing"
)

func fakeLook(present ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, p := range present {
			if p == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestInstallArgv(t *testing.T) {
	pkgs := []string{"cage", "ffmpeg"}
	cases := []struct {
		name    string
		present []string
		want    []string
	}{
		{"arch", []string{"pacman"}, []string{"pacman", "-S", "--needed", "--noconfirm", "cage", "ffmpeg"}},
		{"opensuse", []string{"zypper"}, []string{"zypper", "--non-interactive", "install", "--no-recommends", "cage", "ffmpeg"}},
	}
	for _, c := range cases {
		got, err := InstallArgv(fakeLook(c.present...), pkgs)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
	if _, err := InstallArgv(fakeLook("apt"), pkgs); err == nil {
		t.Error("no known package manager: want an error")
	}
}

func TestPackagesCoverTheNewTools(t *testing.T) {
	want := map[string]string{"cage": "cage", "ffmpeg": "ffmpeg", "wl-clipboard": "wl-copy"}
	for _, p := range Packages {
		if want[p.Package] == p.Binary {
			delete(want, p.Package)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing packages: %v", want)
	}
}

func TestMissingFilePackage(t *testing.T) {
	saved := Packages
	defer func() { Packages = saved }()
	Packages = []Package{
		{Package: "present-file", File: "setup_test.go"},
		{Package: "absent-file", File: "/nonexistent/hyprcage/eng.traineddata"},
	}
	if got := Missing(); !reflect.DeepEqual(got, []string{"absent-file"}) {
		t.Fatalf("Missing() = %v, want [absent-file]", got)
	}
}

func TestInstallArgvZypperNames(t *testing.T) {
	got, _ := InstallArgv(fakeLook("zypper"), []string{"cage", "tesseract", "tesseract-data-eng"})
	want := []string{"zypper", "--non-interactive", "install", "--no-recommends", "cage", "tesseract-ocr", "tesseract-ocr-traineddata-english"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got, _ = InstallArgv(fakeLook("pacman"), []string{"tesseract", "tesseract-data-eng"})
	if want := []string{"pacman", "-S", "--needed", "--noconfirm", "tesseract", "tesseract-data-eng"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
