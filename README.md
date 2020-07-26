```
Usage: gomock [-h] [-d value] [-p value] <interface>
Generates mocks for a go interface. Full path to package can be used.

<interface>
    PackageName.InterfaceName
	PackagePath.InterfaceName

Examples:
    gomock hash.Hash
    gomock github.com/path/package.InterfaceName

    gomock --package mymocks io.Reader
    gomock --directory $GOPATH/src/github.com/josharian/impl hash.Hash

 -d, --directory=value
             package source directory, useful for vendored code
 -h, --help  Help
 -p, --package=value
             package name
```

```instal
go get -u github.com/josharian/impl
```

original forked code https://github.com/josharian/impl
