# AIS Revizor Licensing and Source Availability

**Effective date:** August 4, 2026
**Policy version:** 1.0

## 1. Purpose

AIS Revizor is built for developers and AI coding agents that need reliable runtime visibility.

Because AIS Revizor runs within or alongside a developer’s environment, we believe users should be able to inspect the software they depend on. At the same time, ongoing development, maintenance, security work, support, and product evolution require a sustainable commercial model.

Our licensing and source-publication model is designed to provide both:

* immediate access to current releases through an active subscription;
* predictable publication of source code after a defined delay;
* transparent access to published versions;
* long-term availability under the licensing terms attached to each published version.

This document explains how AIS Revizor releases are distributed, when source code becomes publicly available, and which licensing terms apply.

## 2. Licensing model at a glance

AIS Revizor uses a delayed source-publication model.

| Component or release              | Availability                                                                       |
| --------------------------------- | ---------------------------------------------------------------------------------- |
| AIS Revizor 0.1.0                 | Source code is published immediately                                               |
| Current commercial releases       | Available to active subscribers as official distributions                          |
| Releases after 0.1.0              | Source code is published six months after the official release date                |
| Published AIS Revizor source code | Licensed under FSL-1.1-ALv2                                                        |
| Python SDK                        | Published under FSL-1.1-ALv2 together with the corresponding public source release |
| TypeScript SDK                    | Published under FSL-1.1-ALv2 together with the corresponding public source release |
| Future SDKs                       | Follow the same policy unless explicitly stated otherwise                          |
| Future License                    | Apache License 2.0, as specified by the applicable FSL terms                       |

AIS Revizor is distributed under a **Fair Source / source-available model** for the period during which the Functional Source License applies.

AIS Revizor is not described as Open Source during the FSL period.

## 3. AIS Revizor 0.1.0

The source code of AIS Revizor version 0.1.0 is published immediately.

Version 0.1.0 is licensed under:

> **Functional Source License, Version 1.1, Apache License 2.0 Future License**
> SPDX identifier: `FSL-1.1-ALv2`

The permissions, restrictions, and future-license terms applicable to version 0.1.0 are defined by the complete license text included with that version.

## 4. Future AIS Revizor releases

Beginning with releases after version 0.1.0, AIS Revizor follows a six-month source-publication schedule.

For each commercial release:

1. The release is made available to active subscribers as an official AIS Revizor distribution.
2. The source code for that release remains non-public for six months after the official release date.
3. After the six-month publication period, the corresponding source code is published in the public AIS Revizor source repository.
4. The published source code is licensed under FSL-1.1-ALv2.
5. The future license applicable to the published source code is determined by the FSL terms included with that release.

The six-month publication schedule applies independently to each release.

The FSL Change Date for each published source release is determined from the date on which the source code for that release is publicly published, not from the date of the corresponding commercial release. The exact Change Date is stated in the license file included with the published release.

For example:

| Release | Official release date | Planned source publication date |
| ------- | --------------------- | ------------------------------- |
| 0.2.0   | January 15, 2027      | July 15, 2027                   |
| 0.2.1   | March 1, 2027         | September 1, 2027               |
| 0.3.0   | April 20, 2027        | October 20, 2027                |

The dates in this example are illustrative only.

## 5. Active subscriptions

An active AIS Revizor subscription provides access to the current commercial release line.

Depending on the selected subscription plan, this may include:

* access to current AIS Revizor releases;
* official prebuilt distributions;
* supported installation methods;
* access to new releases during the active subscription period;
* license activation;
* product updates;
* documentation;
* support included with the selected plan;
* additional commercial, team, or enterprise capabilities where applicable.

An active subscription allows users to receive current official releases without waiting for the source-publication period.

## 6. Subscription expiration

AIS Revizor is intended to be a dependable local development tool.

Unless a specific subscription agreement states otherwise, expiration or cancellation of a subscription does not revoke the user’s right to continue using the last AIS Revizor version obtained during an active subscription period.

After a subscription expires:

* the user may continue using the last version obtained during the active subscription period, subject to the applicable commercial license;
* access to newer commercial releases ends;
* access to subscription-specific services, updates, or support may end;
* the user may renew or start a new subscription to regain access to current commercial releases.

The exact rights associated with official commercial distributions are governed by the applicable commercial license and subscription terms.

## 7. Published source code

When a release becomes eligible for source publication, the corresponding source code is published in the public AIS Revizor source repository.

Published source code may include:

* the AIS Revizor core;
* release-specific source files;
* build and packaging files required to understand or reproduce the published release;
* documentation relevant to the published version;
* SDK source code published under the applicable release policy;
* license and third-party notice files.

Each published version includes its own applicable license information.

The public repository may contain multiple versions with different publication dates. Each published version retains the licensing terms included with that version.

## 8. SDK licensing

AIS Revizor SDKs are included with official AIS Revizor distributions.

SDK source code is published according to the following policy.

### Python SDK

The Python SDK is published under FSL-1.1-ALv2 together with the corresponding publicly published AIS Revizor source release.

### TypeScript SDK

The TypeScript SDK is published under FSL-1.1-ALv2 together with the corresponding publicly published AIS Revizor source release.

### Future SDKs

Future SDKs, including the planned Go SDK, follow the same licensing and publication policy unless the documentation for a specific SDK release explicitly states otherwise.

Each published SDK version includes its applicable license information.

## 9. Official binary distributions

Official AIS Revizor distributions may include:

* the AIS Revizor executable;
* Python, TypeScript, Go, or other SDKs;
* default configuration files;
* installation and uninstallation tools;
* documentation;
* license information;
* release-specific metadata.

Official commercial distributions may contain a persistent internal identifier associated with the authorized AIS Platform account.

The identifier does not contain a user’s name, email address, contact information, or other personal data. It does not affect the functional behavior of AIS Revizor.

The identifier is used solely for license administration, distribution integrity, and investigation of unauthorized redistribution.

## 10. Published source code and official distributions

Public availability of source code does not require AIS Platform to provide:

* free access to current commercial binaries;
* free access to future releases before their source-publication date;
* commercial support;
* subscription services;
* hosted services;
* subscription-specific features;
* personalized distributions;
* enterprise services;
* custom builds;
* compatibility guarantees beyond those explicitly stated.

Users may inspect, modify, and build published source code subject to the terms of the license included with the applicable version.

AIS Platform does not guarantee that a user-built version is identical to an official commercial distribution unless reproducible-build support is explicitly provided for that release.

Official commercial distributions may include release packaging, installation components, license activation, distribution metadata, or other elements not present in the published source repository.

## 11. Security and transparency

AIS Revizor is designed to operate within the user’s environment.

For published source versions, users may independently inspect relevant aspects of the software, including:

* application behavior;
* data handling;
* local storage behavior;
* network behavior;
* installation logic;
* SDK behavior;
* configuration handling;
* trace processing;
* sanitization mechanisms.

Source publication is intended to support independent review, technical evaluation, and informed adoption.

Publication of source code does not constitute:

* a security warranty;
* a security certification;
* a guarantee that the software is free from defects;
* a guarantee that the software is free from vulnerabilities;
* a guarantee that a self-built version is identical to an official distribution.

## 12. Contributions

AIS Platform may accept:

* bug reports;
* security reports;
* documentation improvements;
* feature proposals;
* code contributions.

Contribution procedures may be defined in:

* `CONTRIBUTING.md`;
* repository contribution guidelines;
* issue templates;
* pull-request templates;
* contributor agreements, if introduced.

Unless explicitly stated otherwise, submitting a contribution does not grant the contributor ownership of AIS Revizor, AIS Platform, or associated trademarks.

## 13. Trademarks and product identity

“AIS Platform,” “AIS Revizor,” associated logos, and other AIS Platform brand identifiers may be protected trademarks or brand assets.

The applicable source-code license grants rights to the source code under its terms. It does not grant permission to use AIS Platform trademarks in a way that suggests:

* endorsement;
* affiliation;
* official certification;
* official support;
* ownership of an unofficial fork;
* distribution of a modified product as an official AIS Revizor release.

Modified or independently distributed versions must not be presented as official AIS Revizor releases without written permission from AIS Platform.

## 14. Third-party software

AIS Revizor may include or depend on third-party software.

Third-party components remain subject to their own license terms.

Applicable third-party notices and license information are provided in the relevant distribution, source repository, or release documentation.

Nothing in this policy changes the rights or obligations established by third-party licenses.

## 15. Changes to this policy

AIS Platform may update this licensing and source-publication policy for future releases.

Changes to this policy do not retroactively remove rights already granted under the license attached to a published version.

Each published source version retains the license and future-license terms included with that version.

The licensing status of a specific version must be determined from:

1. the license file included with that version;
2. the release metadata for that version;
3. the applicable commercial license or subscription agreement, where relevant.

## 16. Authoritative license terms

This document provides a plain-language explanation of the AIS Revizor licensing and source-publication model.

This document is not itself a software license.

For published source code, the legally controlling terms are the complete license text included with the applicable version.

Published AIS Revizor source code is licensed under:

> **Functional Source License, Version 1.1, Apache License 2.0 Future License**
> SPDX identifier: `FSL-1.1-ALv2`

Where this document conflicts with the license included with a specific published version, the license text included with that version controls.

Commercial use of official AIS Revizor distributions is governed by the applicable commercial license and subscription terms.

## 17. Questions

Questions concerning:

* licensing;
* subscriptions;
* commercial use;
* source publication;
* SDK licensing;
* enterprise licensing;
* official distributions;

may be submitted through the official AIS Platform support channels.

For legal interpretation or legal advice concerning the use of AIS Revizor, users should consult qualified legal counsel.
