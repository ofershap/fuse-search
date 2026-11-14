# Security Policy

## Reporting a vulnerability

Please report security issues through GitHub's private vulnerability reporting
("Report a vulnerability" on the Security tab of this repository). Do not open
a public issue for a vulnerability.

## Scope notes

fuse-search runs pattern matching over caller-supplied data in process: it
performs no network calls, executes no code, and evaluates no scripts. As in
fuse.js, pathological pattern/input combinations can be slow; bound input
sizes in your own code when searching untrusted data.
