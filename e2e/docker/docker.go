// Copyright (c) Contributors to the Apptainer project, established as
//   Apptainer a Series of LF Projects LLC.
//   For website terms of use, trademark policy, privacy policy and other
//   project policies see https://lfprojects.org/policies
// Copyright (c) 2019-2021 Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apptainer/apptainer/e2e/internal/e2e"
	"github.com/apptainer/apptainer/e2e/internal/testhelper"
	"github.com/apptainer/apptainer/internal/pkg/test/tool/require"
	"github.com/apptainer/apptainer/internal/pkg/util/fs"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

type ctx struct {
	env e2e.TestEnv
}

func (c ctx) testDockerPulls(t *testing.T) {
	const tmpContainerFile = "test_container.sif"

	tmpPath, err := fs.MakeTmpDir(c.env.TestDir, "docker-", 0o755)
	err = errors.Wrapf(err, "creating temporary directory in %q for docker pull test", c.env.TestDir)
	if err != nil {
		t.Fatalf("failed to create temporary directory: %+v", err)
	}
	defer os.RemoveAll(tmpPath)

	tmpImage := filepath.Join(tmpPath, tmpContainerFile)

	tests := []struct {
		name    string
		options []string
		image   string
		uri     string
		exit    int
	}{
		{
			name:  "AlpineLatestPull",
			image: tmpImage,
			uri:   "docker://alpine:latest",
			exit:  0,
		},
		{
			name:  "Alpine3.9Pull",
			image: filepath.Join(tmpPath, "alpine.sif"),
			uri:   "docker://alpine:3.9",
			exit:  0,
		},
		{
			name:    "Alpine3.9ForcePull",
			options: []string{"--force"},
			image:   tmpImage,
			uri:     "docker://alpine:3.9",
			exit:    0,
		},
		{
			name:    "BusyboxLatestPull",
			options: []string{"--force"},
			image:   tmpImage,
			uri:     "docker://busybox:latest",
			exit:    0,
		},
		{
			name:  "BusyboxLatestPullFail",
			image: tmpImage,
			uri:   "docker://busybox:latest",
			exit:  255,
		},
		{
			name:    "Busybox1.28Pull",
			options: []string{"--force", "--dir", tmpPath},
			image:   tmpContainerFile,
			uri:     "docker://busybox:1.28",
			exit:    0,
		},
		{
			name:  "Busybox1.28PullFail",
			image: tmpImage,
			uri:   "docker://busybox:1.28",
			exit:  255,
		},
		{
			name:  "Busybox1.28PullDirFail",
			image: "/foo/sif.sif",
			uri:   "docker://busybox:1.28",
			exit:  255,
		},
	}

	for _, tt := range tests {
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("pull"),
			e2e.WithArgs(append(tt.options, tt.image, tt.uri)...),
			e2e.PostRun(func(t *testing.T) {
				if !t.Failed() && tt.exit == 0 {
					path := tt.image
					// handle the --dir case
					if path == tmpContainerFile {
						path = filepath.Join(tmpPath, tmpContainerFile)
					}
					c.env.ImageVerify(t, path, e2e.UserProfile)
				}
			}),
			e2e.ExpectExit(tt.exit),
		)
	}
}

// AUFS sanity tests
func (c ctx) testDockerAUFS(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "aufs-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("build"),
		e2e.WithArgs([]string{imagePath, "docker://sylabsio/aufs-sanity"}...),
		e2e.ExpectExit(0),
	)

	if t.Failed() {
		return
	}

	fileTests := []struct {
		name string
		argv []string
		exit int
	}{
		{
			name: "File 2",
			argv: []string{imagePath, "ls", "/test/whiteout-dir/file2", "/test/whiteout-file/file2", "/test/normal-dir/file2"},
			exit: 0,
		},
		{
			name: "File1",
			argv: []string{imagePath, "ls", "/test/whiteout-dir/file1", "/test/whiteout-file/file1"},
			exit: 1,
		},
	}

	for _, tt := range fileTests {
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("exec"),
			e2e.WithArgs(tt.argv...),
			e2e.ExpectExit(tt.exit),
		)
	}
}

// Check force permissions for user builds #977
func (c ctx) testDockerPermissions(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "perm-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("build"),
		e2e.WithArgs([]string{imagePath, "docker://sylabsio/userperms"}...),
		e2e.ExpectExit(0),
	)

	if t.Failed() {
		return
	}

	fileTests := []struct {
		name string
		argv []string
		exit int
	}{
		{
			name: "TestDir",
			argv: []string{imagePath, "ls", "/testdir/"},
			exit: 0,
		},
		{
			name: "TestDirFile",
			argv: []string{imagePath, "ls", "/testdir/testfile"},
			exit: 1,
		},
	}
	for _, tt := range fileTests {
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("exec"),
			e2e.WithArgs(tt.argv...),
			e2e.ExpectExit(tt.exit),
		)
	}
}

// Check whiteout of symbolic links #1592 #1576
func (c ctx) testDockerWhiteoutSymlink(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "whiteout-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("build"),
		e2e.WithArgs([]string{imagePath, "docker://sylabsio/linkwh"}...),
		e2e.PostRun(func(t *testing.T) {
			if t.Failed() {
				return
			}
			c.env.ImageVerify(t, imagePath, e2e.UserProfile)
		}),
		e2e.ExpectExit(0),
	)
}

func (c ctx) testDockerDefFile(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "def-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	getKernelMajor := func(t *testing.T) (major int) {
		var buf unix.Utsname
		if err := unix.Uname(&buf); err != nil {
			err = errors.Wrap(err, "getting current kernel information")
			t.Fatalf("uname failed: %+v", err)
		}
		n, err := fmt.Sscanf(string(buf.Release[:]), "%d.", &major)
		err = errors.Wrap(err, "getting current kernel release")
		if err != nil {
			t.Fatalf("Sscanf failed, n=%d: %+v", n, err)
		}
		if n != 1 {
			t.Fatalf("Unexpected result while getting major release number: n=%d", n)
		}
		return
	}

	tests := []struct {
		name                string
		kernelMajorRequired int
		archRequired        string
		from                string
	}{
		// This only provides Arch for amd64
		{
			name:                "Arch",
			kernelMajorRequired: 3,
			archRequired:        "amd64",
			from:                "dock0/arch:latest",
		},
		{
			name:                "BusyBox",
			kernelMajorRequired: 0,
			from:                "busybox:latest",
		},
		// CentOS6 is for amd64 (or i386) only
		{
			name:                "CentOS_6",
			kernelMajorRequired: 0,
			archRequired:        "amd64",
			from:                "centos:6",
		},
		{
			name:                "CentOS_7",
			kernelMajorRequired: 0,
			from:                "centos:7",
		},
		{
			name:                "RockyLinux_8",
			kernelMajorRequired: 3,
			from:                "rockylinux:8",
		},
		{
			name:                "Ubuntu_1604",
			kernelMajorRequired: 0,
			from:                "ubuntu:16.04",
		},
		{
			name:                "Ubuntu_1804",
			kernelMajorRequired: 3,
			from:                "ubuntu:18.04",
		},
	}

	for _, tt := range tests {
		deffile := e2e.PrepareDefFile(e2e.DefFileDetails{
			Bootstrap: "docker",
			From:      tt.from,
		})

		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.RootProfile),
			e2e.WithCommand("build"),
			e2e.WithArgs([]string{imagePath, deffile}...),
			e2e.PreRun(func(t *testing.T) {
				require.Arch(t, tt.archRequired)
				if getKernelMajor(t) < tt.kernelMajorRequired {
					t.Skipf("kernel >=%v.x required", tt.kernelMajorRequired)
				}
			}),
			e2e.PostRun(func(t *testing.T) {
				defer os.Remove(imagePath)
				defer os.Remove(deffile)

				if t.Failed() {
					return
				}

				c.env.ImageVerify(t, imagePath, e2e.RootProfile)
			}),
			e2e.ExpectExit(0),
		)
	}
}

func (c ctx) testDockerRegistry(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "registry-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	e2e.EnsureRegistry(t)

	tests := []struct {
		name string
		exit int
		dfd  e2e.DefFileDetails
	}{
		{
			name: "BusyBox",
			exit: 0,
			dfd: e2e.DefFileDetails{
				Bootstrap: "docker",
				From:      "localhost:5000/my-busybox",
			},
		},
		{
			name: "BusyBoxRegistry",
			exit: 0,
			dfd: e2e.DefFileDetails{
				Bootstrap: "docker",
				From:      "my-busybox",
				Registry:  "localhost:5000",
			},
		},
		{
			name: "BusyBoxNamespace",
			exit: 255,
			dfd: e2e.DefFileDetails{
				Bootstrap: "docker",
				From:      "my-busybox",
				Registry:  "localhost:5000",
				Namespace: "not-a-namespace",
			},
		},
	}

	for _, tt := range tests {
		defFile := e2e.PrepareDefFile(tt.dfd)

		c.env.RunApptainer(
			t,
			e2e.WithProfile(e2e.RootProfile),
			e2e.WithCommand("build"),
			e2e.WithArgs("--no-https", imagePath, defFile),
			e2e.PostRun(func(t *testing.T) {
				defer os.Remove(imagePath)
				defer os.Remove(defFile)

				if t.Failed() || tt.exit != 0 {
					return
				}

				c.env.ImageVerify(t, imagePath, e2e.RootProfile)
			}),
			e2e.ExpectExit(tt.exit),
		)
	}
}

// https://github.com/sylabs/singularity/issues/233
func (c ctx) testDockerCMDQuotes(t *testing.T) {
	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("run"),
		e2e.WithArgs("docker://sylabsio/issue233"),
		e2e.ExpectExit(0,
			e2e.ExpectOutput(e2e.ContainMatch, "Test run"),
		),
	)
}

func (c ctx) testDockerLabels(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "labels-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	// Test container & set labels
	// See: https://github.com/sylabs/singularity-test-containers/pull/1
	imgSrc := "docker://sylabsio/labels"
	label1 := "LABEL1: 1"
	label2 := "LABEL2: TWO"

	c.env.RunApptainer(
		t,
		e2e.AsSubtest("build"),
		e2e.WithProfile(e2e.RootProfile),
		e2e.WithCommand("build"),
		e2e.WithArgs(imagePath, imgSrc),
		e2e.ExpectExit(0),
	)

	verifyOutput := func(t *testing.T, r *e2e.ApptainerCmdResult) {
		output := string(r.Stdout)
		for _, l := range []string{label1, label2} {
			if !strings.Contains(output, l) {
				t.Errorf("Did not find expected label %s in inspect output", l)
			}
		}
	}

	c.env.RunApptainer(
		t,
		e2e.AsSubtest("inspect"),
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("inspect"),
		e2e.WithArgs([]string{"--labels", imagePath}...),
		e2e.ExpectExit(0, verifyOutput),
	)
}

//nolint:dupl
func (c ctx) testDockerCMD(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "docker-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("while getting $HOME: %s", err)
	}

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("pull"),
		e2e.WithArgs(imagePath, "docker://sylabsio/docker-cmd"),
		e2e.ExpectExit(0),
	)

	tests := []struct {
		name         string
		args         []string
		noeval       bool
		expectOutput string
	}{
		// Apptainer historic behavior (without --no-eval)
		// These do not all match Docker, due to evaluation, consumption of quoting.
		{
			name:         "default",
			args:         []string{},
			noeval:       false,
			expectOutput: `CMD 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "override",
			args:         []string{"echo", "test"},
			noeval:       false,
			expectOutput: `test`,
		},
		{
			name:         "override env var",
			args:         []string{"echo", "$HOME"},
			noeval:       false,
			expectOutput: home,
		},
		// This looks very wrong, but is historic behavior
		{
			name:         "override sh echo",
			args:         []string{"sh", "-c", `echo "hello there"`},
			noeval:       false,
			expectOutput: "hello",
		},
		// Docker/OCI behavior (with --no-eval)
		{
			name:         "no-eval/default",
			args:         []string{},
			noeval:       false,
			expectOutput: `CMD 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "no-eval/override",
			args:         []string{"echo", "test"},
			noeval:       true,
			expectOutput: `test`,
		},
		{
			name:         "no-eval/override env var",
			noeval:       true,
			args:         []string{"echo", "$HOME"},
			expectOutput: "$HOME",
		},
		{
			name:         "no-eval/override sh echo",
			noeval:       true,
			args:         []string{"sh", "-c", `echo "hello there"`},
			expectOutput: "hello there",
		},
	}

	for _, tt := range tests {
		cmdArgs := []string{}
		if tt.noeval {
			cmdArgs = append(cmdArgs, "--no-eval")
		}
		cmdArgs = append(cmdArgs, imagePath)
		cmdArgs = append(cmdArgs, tt.args...)
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("run"),
			e2e.WithArgs(cmdArgs...),
			e2e.ExpectExit(0,
				e2e.ExpectOutput(e2e.ExactMatch, tt.expectOutput),
			),
		)
	}
}

//nolint:dupl
func (c ctx) testDockerENTRYPOINT(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "docker-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("while getting $HOME: %s", err)
	}

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("pull"),
		e2e.WithArgs(imagePath, "docker://sylabsio/docker-entrypoint"),
		e2e.ExpectExit(0),
	)

	tests := []struct {
		name         string
		args         []string
		noeval       bool
		expectOutput string
	}{
		// Apptainer historic behavior (without --no-eval)
		// These do not all match Docker, due to evaluation, consumption of quoting.
		{
			name:         "default",
			args:         []string{},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "override",
			args:         []string{"echo", "test"},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo test`,
		},
		{
			name:         "override env var",
			args:         []string{"echo", "$HOME"},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo ` + home,
		},
		// Docker/OCI behavior (with --no-eval)
		{
			name:         "no-eval/default",
			args:         []string{},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "no-eval/override",
			args:         []string{"echo", "test"},
			noeval:       true,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo test`,
		},
		{
			name:         "no-eval/override env var",
			noeval:       true,
			args:         []string{"echo", "$HOME"},
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo $HOME`,
		},
	}

	for _, tt := range tests {
		cmdArgs := []string{}
		if tt.noeval {
			cmdArgs = append(cmdArgs, "--no-eval")
		}
		cmdArgs = append(cmdArgs, imagePath)
		cmdArgs = append(cmdArgs, tt.args...)
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("run"),
			e2e.WithArgs(cmdArgs...),
			e2e.ExpectExit(0,
				e2e.ExpectOutput(e2e.ExactMatch, tt.expectOutput),
			),
		)
	}
}

//nolint:dupl
func (c ctx) testDockerCMDENTRYPOINT(t *testing.T) {
	imageDir, cleanup := e2e.MakeTempDir(t, c.env.TestDir, "docker-", "")
	defer cleanup(t)
	imagePath := filepath.Join(imageDir, "container")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("while getting $HOME: %s", err)
	}

	c.env.RunApptainer(
		t,
		e2e.WithProfile(e2e.UserProfile),
		e2e.WithCommand("pull"),
		e2e.WithArgs(imagePath, "docker://sylabsio/docker-cmd-entrypoint"),
		e2e.ExpectExit(0),
	)

	tests := []struct {
		name         string
		args         []string
		noeval       bool
		expectOutput string
	}{
		// Apptainer historic behavior (without --no-eval)
		// These do not all match Docker, due to evaluation, consumption of quoting.
		{
			name:         "default",
			args:         []string{},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s CMD 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "override",
			args:         []string{"echo", "test"},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo test`,
		},
		{
			name:         "override env var",
			args:         []string{"echo", "$HOME"},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo ` + home,
		},
		// Docker/OCI behavior (with --no-eval)
		{
			name:         "no-eval/default",
			args:         []string{},
			noeval:       false,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s CMD 'quotes' "quotes" $DOLLAR s p a c e s`,
		},
		{
			name:         "no-eval/override",
			args:         []string{"echo", "test"},
			noeval:       true,
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo test`,
		},
		{
			name:         "no-eval/override env var",
			noeval:       true,
			args:         []string{"echo", "$HOME"},
			expectOutput: `ENTRYPOINT 'quotes' "quotes" $DOLLAR s p a c e s echo $HOME`,
		},
	}

	for _, tt := range tests {
		cmdArgs := []string{}
		if tt.noeval {
			cmdArgs = append(cmdArgs, "--no-eval")
		}
		cmdArgs = append(cmdArgs, imagePath)
		cmdArgs = append(cmdArgs, tt.args...)
		c.env.RunApptainer(
			t,
			e2e.AsSubtest(tt.name),
			e2e.WithProfile(e2e.UserProfile),
			e2e.WithCommand("run"),
			e2e.WithArgs(cmdArgs...),
			e2e.ExpectExit(0,
				e2e.ExpectOutput(e2e.ExactMatch, tt.expectOutput),
			),
		)
	}
}

// E2ETests is the main func to trigger the test suite
func E2ETests(env e2e.TestEnv) testhelper.Tests {
	c := ctx{
		env: env,
	}

	return testhelper.Tests{
		"AUFS":             c.testDockerAUFS,
		"def file":         c.testDockerDefFile,
		"permissions":      c.testDockerPermissions,
		"pulls":            c.testDockerPulls,
		"registry":         c.testDockerRegistry,
		"whiteout symlink": c.testDockerWhiteoutSymlink,
		"cmd":              c.testDockerCMD,
		"entrypoint":       c.testDockerENTRYPOINT,
		"cmdentrypoint":    c.testDockerCMDENTRYPOINT,
		"cmd quotes":       c.testDockerCMDQuotes,
		"labels":           c.testDockerLabels,
	}
}
