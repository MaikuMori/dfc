# Security Policy

Only the latest release receives security fixes.

## Reporting a vulnerability

Report privately via [GitHub security advisories](https://github.com/MaikuMori/dfc/security/advisories/new) or email mikskalnins@maikumori.com. Please don't open a public issue for security reports.

## Verifying releases

Release artifacts are signed with cosign (keyless, via GitHub Actions OIDC) and ship with SPDX SBOMs — see the `.bundle` and `.sbom.spdx.json` files attached to each release.
