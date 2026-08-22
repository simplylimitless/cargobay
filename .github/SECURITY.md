# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

We take the security of Cargobay seriously. If you've found a security vulnerability, please follow these steps:

### Responsible Disclosure

1. **Do not** create a public GitHub issue for security vulnerabilities
2. Email your findings to: security@cargobay.example.com
3. Include as much detail as possible to help us reproduce the issue:
   - Steps to reproduce
   - Affected versions
   - Potential impact
   - Any suggested fixes

### What to Expect

- **Within 48 hours**: You'll receive an acknowledgment of your report
- **Within 7 days**: You'll receive our initial assessment and a tentative timeline
- **Within 30 days**: We'll either publish a fix or provide an update
- **After fix**: We'll credit you (with your permission) in the release notes

### Security Measures

Cargobay implements the following security measures:

- Password hashing with bcrypt
- Role-based access control (RBAC)
- Token-based authentication with short-lived JWTs
- SQL injection prevention via parameterized queries
- Rate limiting to prevent brute-force attacks
- Audit logging for security-sensitive operations

### Security Best Practices

When deploying Cargobay:

1. Always use HTTPS in production
2. Keep your Docker images updated
3. Use strong passwords for all accounts
4. Enable RBAC and limit admin access
5. Regularly review audit logs
6. Keep dependencies updated

## Security Updates

Security updates are included in regular releases. To stay secure:

```bash
# Pull the latest version
docker pull ghcr.io/cargobay/cargobay:latest

# Or update via package manager
go get -u github.com/cargobay/cargobay
```

## Penetration Testing

If you plan to conduct security testing:

1. **Obtain written permission** first
2. Follow responsible disclosure guidelines
3. Do not disrupt production services
4. Report all findings through official channels

## Acknowledgments

We would like to thank the following security researchers for their responsible disclosure:

- [List of researchers]

## Contact

- Security Team: security@cargobay.example.com
- General Support: support@cargobay.example.com
- Documentation: https://docs.cargobay.example.com
