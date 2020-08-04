```
Usage: gomock [-h] [-d value] [-p value] <package> <interface>
Generates mocks for a go interface. 

<package>
	Package name or path to the package of interface to mock

<interface>
	Name of the interface to mock

Examples:
    gomock hash Hash
    gomock golang.org/x/tools/godoc/analysis Link 

    gomock --package testutils io Reader
    gomock --directory $GOPATH/src/github.com/pedrogameiro/gomock hash Hash

 -d, --directory=value
             package source directory, useful for vendored code
 -h, --help  Help
 -p, --package=value
             package name
```

```instal
go get -u github.com/pedrogameiro/gomock
```

original forked code https://github.com/josharian/impl
