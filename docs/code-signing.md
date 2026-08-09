# Windows release signing

Releases are Authenticode-signed. Which certificate signs them depends on what
the repository has configured, and the release workflow picks the best available
option automatically.

## 1. Azure Trusted Signing (in use since 2.2.2)

A publicly trusted signature issued per-release by Microsoft. No private key
ever exists on the runner or in this repository; the workflow authenticates to
Azure with GitHub OIDC and the signing service holds the key.

The workflow enables this path only when the repository variable
`AZURE_SIGNING_ACCOUNT` is set. Required configuration:

| Kind | Name | Value |
|---|---|---|
| Variable | `AZURE_SIGNING_ENDPOINT` | Account URI, e.g. `https://eus.codesigning.azure.net` |
| Variable | `AZURE_SIGNING_ACCOUNT` | Trusted Signing account name |
| Variable | `AZURE_SIGNING_PROFILE` | Certificate profile name |
| Secret | `AZURE_TENANT_ID` | Entra tenant ID |
| Secret | `AZURE_CLIENT_ID` | App registration client ID |
| Secret | `AZURE_SUBSCRIPTION_ID` | Subscription holding the signing account |

The app registration needs the **Artifact Signing Certificate Profile Signer**
role on the signing account and a federated credential whose subject matches
this repository's `release` environment. That is why the release job declares
`environment: release`; removing it breaks authentication.

Before any of that works, the Azure account itself needs a **completed identity
validation** and a **certificate profile**. Identity validation is reviewed by
Microsoft and requires legal identity documents; it cannot be scripted.

## 2. Purchased certificate (fallback)

Set the `WINDOWS_SIGNING_CERT_BASE64` and `WINDOWS_SIGNING_CERT_PASSWORD`
secrets to a base64 PFX and its password. The workflow writes the PFX to the
runner's temporary directory, signs, and deletes it.

## 3. Self-signed (used through 2.2.1)

With neither of the above configured, the release is signed with a certificate
the workflow generates on the runner. The signature protects the executable
against changes after signing, but the certificate is **not publicly trusted**.
Windows may still show Unknown Publisher, SmartScreen, or an
untrusted-certificate warning. Releases up to and including 2.2.1 were signed
this way; the notes below describe what those archives contain.

The release archive includes:

- `SELF-SIGNED-CERTIFICATE.cer`: the public certificate only; it contains no
  private signing key.
- `SELF-SIGNED-CERTIFICATE.txt`: its subject, validity, thumbprint, and SHA-256.

Verify the release ZIP against the separately published `SHA256SUMS.txt` and,
when available, the GitHub artifact attestation. The certificate inside the ZIP
does not independently prove who published the ZIP. Do not install it into a
trusted certificate store unless you have independently verified its fingerprint
and accept that trust decision.

Maintainers reproduce this build locally with:

```powershell
./scripts/build-release.ps1 -Version 2.2.1
./scripts/sign-self-signed-release.ps1 -Version 2.2.1
```

The private key remains in the maintainer's Windows Current User certificate
store for reuse and is never added to the repository or release archive.
