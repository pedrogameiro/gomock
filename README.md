```
Usage: gomock [-h] [-d value] [-p value] <interface>
gomock generates mocks for a go interface.

Examples:
    gomock --package mymocks io.Reader
    gomock hash.Hash
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
