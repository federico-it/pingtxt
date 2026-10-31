# Security policy

## Supported versions

Security fixes are released for the **latest release** and land on `main`. Please upgrade to the latest
version before reporting, and keep agents on the same version as the hub (they update themselves by default).

## Reporting a vulnerability

**Please do not open a public issue, discussion or pull request for security problems.**

Report it privately through GitHub:
[**Report a vulnerability**](https://github.com/federico-it/pingtxt/security/advisories/new)
(repository → **Security** → **Report a vulnerability**). Only the maintainers can see the report.

Include as much as you can:

- the affected version and component (hub, agent, web UI, install script, status pages, API);
- a description of the issue and its impact;
- steps or a proof of concept to reproduce it;
- any suggested fix.

## What to expect

- We acknowledge the report within **3 working days** and send a first assessment within **10 days**.
- We work on a fix in a private advisory and keep you posted. You can be invited to collaborate on it.
- Once a fixed release is out we publish the advisory (with a CVE when it applies) and credit you, unless
  you prefer to stay anonymous.
- Please give us reasonable time to release a fix before disclosing the issue publicly. We aim for
  90 days at most.

## Scope

In scope: the pingtxt hub, the agent, the agent install script, the web UI, public status pages and the
Docker images published under `ghcr.io/federico-it`.

Out of scope: problems that need an already compromised host or admin account, missing hardening of
your own reverse proxy or TLS setup, denial of service by sending large amounts of traffic, and reports
from automated scanners without a working impact.
