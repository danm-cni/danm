
# Contributing to DANM

### First of all, a big thank you!

For years we have been thinking that no one would be interested in our internal Kubernetes networking enhancements for TelCo applications.
We greatly appreciate you showing interest in contributing to our project, and thus proving that we were wrong for so long!

### Do we accept contributions?

Absolutely!
We are open to all kinds of community feedback: it need not even be code, we are always happy to talk about requirements, or compare notes with our fellow networking enthusiasts.
Of course, pull requests containing small code or documentation corrections, or even the implementation of new features are also very much welcomed!
This work is released under the 3-Clause BSD License.

### When you are planning a contribution

Regardless whether your contribution is related to an already existing Issue, or not: it is generally a good idea to first discuss it with the core team.
You can expect a response/engagement from the core team within a couple of days, so do not hesitate to contact us first through the [communication channels described below](#communication-channels-besides-github)

Aligning expectations and discussing design ideas before implementing a feature can go a long way in increasing the chances of its acceptance!

Feel free to comment on existing issues, open new ones, or simply send your ideas directly to us!

### Code of conduct
We promise that we will be always transparent when communicating with the community, and will respectfully hear each and every idea out!
In exchange we expect the same behavior from our Contributors.

However, please also note that DANM is already being used in many, vastly differing TelCo production cases.
As a result, the maintainers of the project retain the right of refusing certain contributions, if these would not be compatible with an existing production use-case.

Don't feel discouraged or offended if this would happen to you!
What we can promise is that we will always respectfully and truthfully explore and entertain your reasoning before making a decision, and will always openly and transparently share our own with you in case of a conflict!

### Getting started
So, you are adamant you want to contribute to our project, and maybe even already discussed your idea with us. Awesome! What now?

Keep in mind that the project is:

 - written in Golang and using `go module` feature for dependency management, so you will need a properly set-up `Golang 1.12+`  development environment
 - build scripts depend on `docker`, to be able to run scripts locally you will have to install it on your machine

Once you have the prerequisites, fork our project, code your changes, test your contribution, then start a normal GitHub review process.

Pull requests will be only merged once at least one of the project maintainers approved them.
The minimum expectation towards all pull requests is that:
1. Code is written in accordance with generic Clean Code guidelines
2. The Makefile in the project's root directory successfully executes, all binaries compile
3. All the not many -contribution opportunity 2.0- existing Unit Tests pass

Not mandatory, *but highly appreciated:*
1. Existing coding style (2 spaces indentation, camelCase/CamelCase naming scheme) maintained
2. New Unit Tests are written to cover newly added (or even legacy) code

When writing Unit Tests we prefer testing the packages through their public interfaces!

We appreciate thorough and detailed commit messages.

We are not allergic to the number of commits it took to create a contribution, you are not required to squash and amend your changes all the time.
However, we require you to break-up big contributions into smaller, functionally coherent pieces. This approach greatly reduces both integration and review efforts!
### Future plans
The following topics are on our mind right now, so if you are looking for topic to start with these are as good as any!

Being a new project, we have not yet integrated the repository to an automated CI system (like Travis).

Increasing UT coverage of existing code is alway appreciated.

Extending the "reach" of the DANM ecosystem is our primary goal!
This includes both native, first-class integration of additional CNI plugin interfaces, and integrating more one-network Kubernetes features (e.g. NetworkPolicy) with our DanmNet API.

# Community
### Maintainers / core team
Róbert Springer (@rospring)
Levente Kálé (@Levovar)
### Distinguished contributors
Lengyel Krisztián (@klengyel)
Ferenc Tóth (@TothFerenc)
### Honorable mentions
@peterszilagyi, @libesz, @visnyei, @CsatariGergely, @clivez, @Fillamug, @janosi

Please keep in mind we live in the CET/CEST timezone!

### Communication channels besides GitHub
You can contact the core team mainly via email at robert.springer@nokia.com and levente.kale@gmail.com or you can join to our [slack channel](https://danmws.slack.com) using [this](https://join.slack.com/t/danmws/shared_invite/enQtNzEzMTQ4NDM2NTMxLTA3MDM4NGM0YTRjYzlhNGRiMDVlZWRlMjdlNTkwNTBjNWUyNjM0ZDQ3Y2E4YjE3NjVhNTE1MmEyYzkyMDRlNWU) invite link.

But we also do hang around various Kubernetes slack channels, you might get lucky if you look around networking, node, or resource management :)
