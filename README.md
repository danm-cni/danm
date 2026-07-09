
# DANM
[![Build Status](https://travis-ci.com/nokia/danm.svg?branch=master)](https://travis-ci.com/Nokia/danm)
[![Unit Test Coverage](https://coveralls.io/repos/github/nokia/danm/badge.svg?branch=master)](https://coveralls.io/github/nokia/danm?branch=master)

<img src="https://github.com/danm-cni/danm/raw/master/logo_w_name.png" width="100">

# Join our community!
Want to hang-out with us? Join our Slack under https://danmws.slack.com/!

Feel yourself officially invited by clicking on [this](https://join.slack.com/t/danmws/shared_invite/enQtNzEzMTQ4NDM2NTMxLTA3MDM4NGM0YTRjYzlhNGRiMDVlZWRlMjdlNTkwNTBjNWUyNjM0ZDQ3Y2E4YjE3NjVhNTE1MmEyYzkyMDRlNWU) link!

# Want to get more bang for the buck? Check out DANM Utils too!
DANM Utils is the home to independet Operators built on top of the DANM network management platform, providing value added services to your cluster!
Interested in adding outage resiliency to your IPAM, or universal network policy support? Look no further and hop over to https://github.com/danm-cni/danm-utils today!

## Table of Contents
* [Table of Contents](#table-of-contents)
* [Introduction](#introduction)
     * [Install an Akraino REC and get DANM for free](#install-an-akraino-rec-and-get-danm-for-free)
* [Our philosophy and motivation behind DANM](#our-philosophy-and-motivation-behind-danm)
* [Scope of the project](#scope-of-the-project)
* [Deployment](#deployment)
* [User guide](#user-guide)
* [Contributing](#contributing)
* [Authors](#authors)
* [License](#license)


## Introduction

__DANM__ was originally developed as Nokia's solution to bring TelCo grade network management into a Kubernetes cluster! DANM had more than 4 years of history inside the company and was deployed into production before it was open sourced.

DANM was used in Nokia's own Telco grade Kubernetes offering for many years. Later Nokia decided to ramp down the Kubernetes platform business and the support for DAMN. In 2026 DANM  moved out from Nokia's stewardship to be maintained by the community.

The name stands for "Damn, Another Network Manager!", because yes, we know: the last thing the K8s world needed is another TelCo company "revolutionizing" networking in Kubernetes.
But still we hope that potential users checking out our project will involuntarily proclaim "DANM, that's some good networking stuff!" :)

Please consider for a moment that there is a whole other world out there, with special requirements, and DANM is the result of those needs!
We are certainly not saying DANM is __THE__ network solution, but we think it is a damn good one!
Want to learn more about this brave new world? Don't hesitate to contact us, we are always quite happy to share the special requirements we need to satisfy each and every day.

**In any case, DANM is more than just a plugin, it is an End-To-End solution to a whole problem domain**.
It is:
* a CNI plugin capable of provisioning IPVLAN interfaces with advanced features
* an in-built IPAM module with the capability of managing multiple, ***cluster-wide***, discontinuous L3 networks with managing up to 8M allocations per network! plus providing dynamic, static, or no IP allocation scheme on-demand for both IPv4, and IPv6
* a CNI metaplugin capable of attaching multiple network interfaces to a container, either through its own CNI, or through delegating the job to any of the popular CNI solution e.g. SR-IOV, Calico, Flannel etc. ***in parallel***
* a Kubernetes controller capable of centrally managing both VxLAN and VLAN interfaces of all Kubernetes hosts
* another Kubernetes controller extending Kubernetes' Service-based service discovery concept to work over all network interfaces of a Pod
* a standard Kubernetes Validating and Mutating Webhook responsible for making you adhere to the schemas, and also automating network resource management for tenant users in a production-grade environment
### Install an Akraino REC and get DANM for free!
Just kidding as DANM is always free, but if you want to install a production grade, open-source Kubernetes-based bare metal CaaS infrastructure by default equipped with DANM **and** with a single click of a button nonetheless; just head over to Linux Foundation Akraino Radio Edge Cloud (REC) wiki for the [Akraino REC Architecture](https://wiki.akraino.org/display/AK/REC+Architecture+Document) and the [Akraino REC Installation Guide](https://wiki.akraino.org/display/AK/REC+Installation+Guide)
Not just for TelCo!

The above functionalities are implemented by the following components:
- **danm** is the CNI plugin which can be directly integrated with kubelet. Internally it consists of the CNI metaplugin, the CNI plugin responsible for managing IPVLAN interfaces, and the in-built IPAM plugin.
Danm binary is integrated to kubelet as any other [CNI plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/).

- **fakeipam** is a little program used in natively integrating 3rd party CNI plugins into the DANM ecosystem. It is basically used to echo the result of DANM's in-built IPAM to CNIs DANM delegates operations to.
Fakeipam binary should be placed into kubelet's configured CNI plugin directory, next to danm.
Fakeipam is a temporary solution, the long-term aim is to separate DANM's IPAM component into a full-fledged, standalone IPAM solution.

- **netwatcher** is a Kubernetes Controller watching the Kubernetes API for changes in the DANM related CRD network management APIs.
This component is responsible for validating the semantics of network objects, and also for maintaining VxLAN and VLAN host interfaces of all Kubernetes nodes.
Netwatcher binary is deployed in Kubernetes as a DaemonSet, running on all nodes.

- **svcwatcher** is another Kubernetes Controller monitoring Pod, Service, Endpoint, and DanmEp API paths.
This Controller is responsible for extending Kubernetes native Service Discovery to work even for the non-primary networks of the Pod.
Svcwatcher binary is deployed in Kubernetes as a DaemonSet, running only on the Kubernetes master nodes in a clustered setup.

- **webhook** is a standard Kubernetes Validating and Mutating Webhook. It has multiple, crucial responsibilities:

 - it validates all DANM introduced CRD APIs both syntactically, and semantically both during creation, and modification
 - it automatically mutates parameters only relevant to the internal implementation of DANM into the API objects
 - it automatically assigns physical network resources to the logical networks of tenant users in a production-grade infrastructure

## Our philosophy and motivation behind DANM
It is undeniable that TelCo products- even in containerized format- ***must*** own physically separated network interfaces, but we have always felt other projects put too much emphasis on this lone fact, and entirely ignored -or were afraid to tackle- the larger issue with Kubernetes.
That is: capability to **provision** multiple network interfaces to Pods is a very limited enhancement if the cloud native feature of Kubernetes **cannot be used with those extra interfaces**.

This is the very big misconception our solution aims to rectify - we strongly believe that all network interfaces shall be natively supported by K8s, and there are no such things as "primary", or "secondary" network interfaces.
Why couldn't NetworkPolicies, Services, LoadBalancers, all of these existing and proven Kubernetes constructs work with all network interfaces?
Why couldn't network administrators freely decide which physical networks are reachable by a Pod?
In our opinion the answer is quite simple: because networks are not first-class citizens in Kubernetes.

This is the historical reason why DANM's CRD based, abstract network management APIs were born, and why is the whole ecosystem built around the concept of promoting networks to first-class Kubernetes API objects.

This approach opens-up a plethora of possibilities, even with today's Kubernetes core code!

The following chapters will guide you through the description of these features, and will show you how you can leverage them in your Kubernetes cluster.
## Scope of the project
You will see at the end of this README that we really went above and beyond what "networks" are in vanilla Kubernetes.

But, DANM core project never did, and will break one core concept: DANM is first and foremost a run-time agnostic standard CNI system for Kubernetes, 100% adhering to the Kubernetes life-cycle management principles.

It is important to state this, because the features DANM provides open up a couple of very enticing, but also very dangerous avenues:
 - what if we would monitor the run-time and provide added high-availability feature based on events happening on that level?
 - what if we could change the networks of existing Pods?

 We strongly feel that all such scenarios incompatible with the life-cycle of a standard CNI plugin firmly fall outside the responsibility of the core DANM project.
That being said, tell us about your Kubernetes breaking ideas! We are open to accept such plugins into the wider umbrella of the existing eco-system: outside of the core project, but still loosely linked to suite as optional, external components.
Just because something doesn't fit into core DANM, it does not mean it can't fit into your cloud!
Please visit [DANM utils](https://github.com/danm-cni/danm-utils) repository for more info.


## Deployment
See [Deployment Guide](deployment-guide.md).

## User guide
See [User Guide](user-guide.md).

## Contributing

Please read [CONTRIBUTING.md](https://github.com/danm-cni/danm/blob/master/CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

## Authors

* **Robert Springer** (@rospring) - Initial work (V1 Python), IPAM, Netwatcher, Svcwatcher [Nokia](https://github.com/nokia)
* **Levente Kale** (@Levovar) - Initial work (V2 Golang), Documentation, Integration, SCM, UTs, Metaplugin, V4 work (EX)[Nokia](https://github.com/nokia)

Special thanks to the original author who started the whole project in 2015 by putting a proprietary network management plugin between Kubelet and Docker; and also for coining the DANM acronym:
**Peter Braun** (@peter-braun)

## License

This project is licensed under the 3-Clause BSD License - see the [LICENSE](LICENSE)
