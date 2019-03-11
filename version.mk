COMMIT_NUMBER=$(shell git rev-list `git rev-list --parents HEAD | egrep "^[a-f0-9]{40}$$"`..HEAD --count)
BUILD_DATE=$(shell date -u +%Y-%m-%d)
CURRENT_COMMIT=$(shell git rev-parse --short=8 HEAD)
VERSION_FULL=$(VERSION_MAJOR).$(VERSION_MINOR).$(COMMIT_NUMBER)-$(BUILD_DATE)-$(CURRENT_COMMIT)
