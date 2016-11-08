# slo-up


[![standard-readme compliant](https://img.shields.io/badge/standard--readme-OK-green.svg?style=flat-square)](https://github.com/RichardLitt/standard-readme)

> Tool for uploading massive data into OpenStack Object Storage

TODO: Fill out this long description.

## Table of Contents

- [Background](#background)
- [Install](#install)
- [Usage](#usage)
- [Contribute](#contribute)
- [License](#license)

## Background

This utility is an example of using the [swiftlygo pipeline api](https://github.com/ibmjstart/swiftlygo) to build a data uploader for
OpenStack Object Storage.

## Install

```
go get github.com/ibmjstart/slo-up
```

## Usage

### Authentication Flags

The flags that you need to give `slo-up` vary with the authentication version of the Object Storage instance that you're trying to
upload to. If you don't know the authentication version, the credentials that you are provided with should give you a clue.

```
slo-up -url <auth-url> -user <username> -p <password> ...upload-flags...
```

The password is sometimes referred to as an API key.
SoftLayer Object Storage uses this Authentication method.

#### Auth V2

For services supporting Auth V2, you will additionally need to provide a tenant id.
```
slo-up ...auth-flags... -t <tenant-id> ...upload-flags...
```

#### Auth V3

For services supporting Auth V3, you will additionally need to provide a domain id. 
```
slo-up ...auth-flags... -d <domain-name> ...upload-flags...
```

IBM Bluemix uses this type of authentication.

### Upload Flags

The other flags exist to provide upload options:
 - `-c <container>` The name of the container in object storage into which you are uploading data. The container must already exist.
 - `-o <object>` The name of the object in object storage into which you are uploading data. Anything in the targeted container with this name will be overwritten.
 - `-f <path>` The local file that you are uploading.
 - `-z size` Optionally choose the size of each chunk of your file. Defaults to 10^9 bytes.
 - `-e <comma-separated-list>` Optional list of chunk numbers to skip reading. Useful if some file sections are unreadable due to hard drive failure.
 - `-h <hash-file-name>` Optional name of hash file written by a previous run of this utility. Providing this allows the uploader to skip some expensive computation on a second run.
 - `-only-missing` Optional flag that causes uploader to only upload chunks that are not already in object storage. This is determined by checking the names of existing file chunks, and can have false positives (though it's unlikely unless you follow the `slo-up` file chunk naming convention).
 - `-no-color` Optionally turns off the fancy colorized output and disables ANSI redrawing.
 - `-help` Prints usage info and exits.

## Contribute

PRs accepted.

Small note: If editing the README, please conform to the [standard-readme](https://github.com/RichardLitt/standard-readme) specification.

## License
Apache 2.0
 © IBM jStart
