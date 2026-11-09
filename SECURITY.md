# Security policy

## Reporting a vulnerability

Please report security issues privately, not in a public issue. Use [Report a vulnerability](https://github.com/DuncanDoyle/snellius/security/advisories/new) on the repository's Security tab.

Include the Snellius version, the kgateway version, how you installed it, and the steps to reproduce. Remove tokens, kubeconfigs and other secrets from anything you attach.

We acknowledge reports within a few working days and keep you updated until a fix is released.

## Supported versions

Fixes go into the latest release. Upgrade to it before you report, where you can.

## Scope

Snellius is read-only and has no login yet. Anyone who can reach the UI sees what its ServiceAccount can read. That is documented behaviour, not a vulnerability; the [security and trust model](README.md#security-and-trust-model) explains it. A way to read Secrets, change the cluster, or reach data outside the UI's RBAC is in scope.
