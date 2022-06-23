#!/usr/bin/env python

# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
from datetime import datetime
import numpy
import threading
import os
import random
import string
import subprocess
import sys
import time
import yaml


def editWork(y, num):
    test_name = 'test-work-' + ''.join(str(num))
    y['metadata']['name'] = test_name
    y['spec']['workload']['manifests'][0]['metadata']['name'] = test_name

    modified_name = "modified" + str(num) + ".yaml"
    with open(modified_name, "w") as output:
        yaml.dump(y, output, default_flow_style=False, sort_keys=False)

    output.close()
    return modified_name


def execute(names):
    for name in names:
        subprocess.run(["kubectl", "apply", "-f", name])


def main():
    work_num = int(sys.argv[1])
    thread_num = int(sys.argv[2])
    if thread_num > 2000:
        raise Exception()
    with open("example-work.yaml") as f:
        y = yaml.safe_load(f)

    file_list = []
    for x in range(0, int(sys.argv[1])):
        file_list.append(editWork(y, random.randint(10000, 100000)))

    thread_list = []
    for split_array in numpy.array_split(file_list, thread_num):
        thread_list.append(threading.Timer(1 / int(work_num), execute, args=[split_array]))

    for thread in thread_list:
        thread.start()

    for thread in thread_list:
        thread.join()

    for name in file_list:
        try:
            os.remove(name)
        except FileNotFoundError:
            continue


if __name__ == "__main__":
    main()
