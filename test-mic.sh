#!/bin/bash

set -x
set -e

pushd ~/git/CBL-Mariner-POC/toolkit/tools/imagecustomizer
go build
sudo rm -rf /home/george/git/CBL-Mariner-POC/mic-build

sudo ./imagecustomizer \
    --build-dir /home/george/git/CBL-Mariner-POC/mic-build \
    --image-file /home/george/git/CBL-Mariner-POC/baremetal-2.0.20231220.2000.vhdx \
    --output-image-file /home/george/git/CBL-Mariner-POC/mic-build/out/mic-out-image.iso \
    --output-image-format iso \
    --config-file /home/george/git/CBL-Mariner-POC/mic-config.yaml

popd