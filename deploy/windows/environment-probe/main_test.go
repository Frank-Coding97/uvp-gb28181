package main

import "testing"

func TestRuntimeRequiresWindows10NativeX64(t *testing.T) {
	good := inventory{Caption: "Microsoft Windows 10 Pro", Build: 19041, ProductType: 1, Architecture: 9}
	if err := validate("runtime", good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*inventory){
		func(i *inventory) { i.Build = 19040 },
		func(i *inventory) { i.ProductType = 3 },
		func(i *inventory) { i.Architecture = 12 },
		func(i *inventory) { i.Caption = "" },
		func(i *inventory) { i.Caption = "Microsoft Windows 11 Pro" },
		func(i *inventory) { i.BuildTools = []string{"go"} },
		func(i *inventory) { i.RunningComponents = []string{"redis-server"} },
	} {
		bad := good
		change(&bad)
		if err := validate("runtime", bad); err == nil {
			t.Fatalf("accepted unsupported runtime: %+v", bad)
		}
	}
}

func TestBuildAllowsWindowsServerAndTools(t *testing.T) {
	i := inventory{Caption: "Microsoft Windows Server 2022", Build: 20348, ProductType: 3, Architecture: 9, BuildTools: []string{"git", "cmake"}}
	if err := validate("build", i); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownRoleIsRejected(t *testing.T) {
	if err := validate("anything", inventory{}); err == nil {
		t.Fatal("accepted unknown role")
	}
}
