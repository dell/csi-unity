# CSI Driver for Dell Unity XT

[![Go Report Card](https://goreportcard.com/badge/github.com/dell/csi-unity?style=flat-square)](https://goreportcard.com/report/github.com/dell/csi-unity)
[![License](https://img.shields.io/github/license/dell/csi-unity?style=flat-square&color=blue&label=License)](https://github.com/dell/csi-unity/blob/main/LICENSE)
[![Docker](https://img.shields.io/docker/pulls/dellemc/csi-unity.svg?logo=docker&style=flat-square&label=Pulls)](https://hub.docker.com/r/dellemc/csi-unity)
[![Last Release](https://img.shields.io/github/v/release/dell/csi-unity?label=Latest&style=flat-square&logo=go)](https://github.com/dell/csi-unity/releases)

**Repository for CSI Driver for Dell Unity XT**

## Description
CSI Driver for Unity XT is part of the [CSM (Container Storage Modules)](https://github.com/dell/csm) open-source suite of Kubernetes storage enablers for Dell products. CSI Driver for Unity XT is a Container Storage Interface (CSI) driver that provides support for provisioning persistent storage using Dell Unity XT storage array. 

It supports CSI specification version 1.5.

This project may be compiled as a stand-alone binary using Golang that, when run, provides a valid CSI endpoint. It also can be used as a precompiled container image.

## Table of Contents

* [Code of Conduct](https://github.com/dell/csm/blob/main/docs/CODE_OF_CONDUCT.md)
* [Maintainer Guide](https://github.com/dell/csm/blob/main/docs/MAINTAINER_GUIDE.md)
* [Committer Guide](https://github.com/dell/csm/blob/main/docs/COMMITTER_GUIDE.md)
* [Contributing Guide](https://github.com/dell/csm/blob/main/docs/CONTRIBUTING.md)
* [List of Adopters](https://github.com/dell/csm/blob/main/docs/ADOPTERS.md)
* [Support](#support)
* [Security](https://github.com/dell/csm/blob/main/docs/SECURITY.md)
* [Building](#building)
* [Runtime Dependecies](#runtime-dependencies)
* [Driver Installation](#driver-installation)
* [Using Driver](#using-driver)
* [Documentation](#documentation)

## Support
For any CSI driver issues, questions or feedback, please follow our [support process](https://github.com/dell/csm/blob/main/docs/SUPPORT.md)

## Building
This project is a Go module (see golang.org Module information for explanation).
The dependencies for this project are in the go.mod file.

To build the source, execute `make go-build`.

To run unit tests, execute `make unit-test`.

To build a podman based image, execute `make podman-build`.

You can run an integration test on a Linux system by populating the env files at `test/integration/` with values for your Dell Unity XT systems and then run `make integration-test`.


## Runtime Dependencies
Both the Controller and the Node portions of the driver can only be run on nodes which have network connectivity to “`Unisphere for Unity XT`” (which is used by the driver).

## Driver Installation
Please consult the [Installation Guide](https://dell.github.io/csm-docs/docs/csidriver/installation)

## Using Driver
Please refer to the section `Testing Drivers` in the [Documentation](https://dell.github.io/csm-docs/docs/csidriver/installation/test/) for more info.

## Documentation
For more detailed information on the driver, please refer to [Container Storage Modules documentation](https://dell.github.io/csm-docs/).
