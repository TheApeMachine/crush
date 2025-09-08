# Edit Verification Middleware

This document describes the preflight and postflight verification middleware system for edit operations in the Crush agent framework.

## Overview

The Edit Verification Middleware provides comprehensive safety checks for file editing operations, helping prevent accidental code breakage and ensuring code quality. It integrates seamlessly with the existing agent system and TreeSitter-powered tools.

## Features

### Preflight Verification
- **Impact Analysis**: Runs blast radius analysis before edits using TreeSitter
- **Risk Assessment**: Calculates impact scores and risk levels (Low, Medium, High, Critical)
- **Approval Gating**: Blocks high-impact edits when configured to require approval
- **Safety Recommendations**: Provides actionable guidance based on analysis

### Postflight Verification
- **Regression Detection**: Re-runs impact analysis after edits to detect new issues
- **Auto-Rollback**: Automatically suggests rollback for critical regressions
- **Validation**: Ensures no new unresolved references or structural issues

## Architecture

### Core Components

1. **Middleware Interface**: Defines the contract for all middleware implementations
2. **EditVerificationMiddleware**: Main implementation for edit operations
3. **MiddlewareManager**: Manages the chain of middleware execution
4. **Configuration System**: Configurable behavior through agent options

### Integration Points

- **Agent System**: Integrated into the agent initialization and tool execution flow
- **Tool Execution**: Intercepts `edit` and `multiedit` tool calls
- **TreeSitter**: Leverages existing TreeSitter registry for AST analysis
- **Impact Analysis**: Uses the existing ImpactTool for comprehensive analysis

## Configuration

The middleware is configured through the agent's options in the configuration file:

```json
{
  "options": {
    "edit_verification": {
      "enabled": true,
      "blast_radius_threshold": 10,
      "require_approval_for_high": true,
      "auto_rollback_on_error": true,
      "analysis_timeout": "30s"
    }
  }
}
```

### Configuration Options

- `enabled`: Enable/disable the middleware (default: true)
- `blast_radius_threshold`: Maximum blast radius before requiring approval (default: 10)
- `require_approval_for_high`: Require user approval for high-impact edits (default: true)
- `auto_rollback_on_error`: Automatically suggest rollback on regressions (default: true)
- `analysis_timeout`: Timeout for impact analysis operations (default: 30s)

## How It Works

### Preflight Process

1. **Tool Call Interception**: Middleware intercepts `edit` and `multiedit` tool calls
2. **File Path Extraction**: Extracts the target file path from tool parameters
3. **Impact Analysis**: Runs TreeSitter-powered impact analysis on the target file
4. **Risk Assessment**: Calculates blast radius, impact score, and risk level
5. **Decision Making**:
   - Low risk: Allow execution
   - High risk: Block and request approval (if configured)
   - Analysis failure: Allow execution with warning

### Postflight Process

1. **Post-Edit Analysis**: Re-runs impact analysis on the modified file
2. **Regression Detection**: Compares pre and post-edit analysis results
3. **Issue Identification**: Detects new unresolved references or structural issues
4. **Response Generation**:
   - No issues: Allow continuation
   - Issues detected: Suggest rollback (if configured)

## Risk Levels and Recommendations

### Low Risk (Score: 1-5)
- **Description**: Minimal impact, safe to change
- **Recommendations**:
  - Consider adding tests for modified functionality
  - Review affected files for indirect dependencies

### Medium Risk (Score: 6-15)
- **Description**: Moderate impact, test thoroughly
- **Recommendations**:
  - Create checklist of affected components
  - Consider backward compatibility
  - Notify team members

### High Risk (Score: 16-50)
- **Description**: Significant impact, careful review needed
- **Recommendations**:
  - Perform impact analysis on dependent systems
  - Consider feature flags for gradual rollout
  - Plan comprehensive testing
  - Document all changes

### Critical Risk (Score: 50+)
- **Description**: High risk, extensive testing required
- **Recommendations**:
  - Architectural review before changes
  - Deploy during low-traffic periods
  - Prepare rollback strategy
  - Involve stakeholders and get approval
  - Conduct end-to-end testing

## Benefits

### Safety Improvements
- **Prevents Accidental Breakage**: Catches high-impact changes before execution
- **Risk Awareness**: Makes developers aware of change consequences
- **Quality Gates**: Enforces review processes for risky changes

### Operational Benefits
- **Faster Recovery**: Auto-detection of post-edit issues
- **Reduced Downtime**: Prevents deployment of broken code
- **Better Planning**: Informed decision-making based on impact analysis

### Developer Experience
- **Guidance**: Provides actionable recommendations
- **Transparency**: Clear visibility into change risks
- **Flexibility**: Configurable behavior based on project needs

## Usage Examples

### Basic Configuration
```json
{
  "options": {
    "edit_verification": {
      "enabled": true
    }
  }
}
```

### Conservative Configuration
```json
{
  "options": {
    "edit_verification": {
      "enabled": true,
      "blast_radius_threshold": 5,
      "require_approval_for_high": true,
      "auto_rollback_on_error": true
    }
  }
}
```

### Permissive Configuration
```json
{
  "options": {
    "edit_verification": {
      "enabled": true,
      "blast_radius_threshold": 20,
      "require_approval_for_high": false,
      "auto_rollback_on_error": false
    }
  }
}
```

## Testing

The middleware includes comprehensive unit tests covering:

- Middleware initialization and configuration
- Tool call interception and filtering
- Impact analysis parsing
- Risk level calculation
- Regression detection
- Integration with middleware manager

Run tests with:
```bash
go test ./internal/llm/agent/ -v
```

## Future Enhancements

### Planned Features
- **Advanced Impact Analysis**: More sophisticated dependency analysis
- **Custom Risk Rules**: Project-specific risk assessment rules
- **Integration Testing**: Automatic test execution for affected components
- **Change Preview**: Visual diff and impact preview
- **Team Notifications**: Automatic notifications for high-risk changes

### Extensibility
- **Plugin System**: Support for custom middleware implementations
- **Analysis Providers**: Pluggable analysis backends
- **Custom Rules**: Project-specific validation rules

## Troubleshooting

### Common Issues

1. **Middleware Not Activating**
   - Check configuration: ensure `edit_verification.enabled` is `true`
   - Verify agent configuration includes the middleware setup

2. **Impact Analysis Failing**
   - Ensure TreeSitter parsers are properly registered
   - Check file permissions and accessibility
   - Verify analysis timeout is sufficient

3. **False Positives/Negatives**
   - Adjust `blast_radius_threshold` based on project size
   - Review risk level calculations for accuracy
   - Consider project-specific customization

### Debug Logging

Enable debug logging to troubleshoot middleware behavior:
```json
{
  "options": {
    "debug": true
  }
}
```

## Contributing

When contributing to the middleware system:

1. **Add Tests**: Include comprehensive tests for new features
2. **Update Documentation**: Keep this README current with changes
3. **Configuration**: Consider backward compatibility for config changes
4. **Performance**: Ensure analysis operations don't significantly impact response times
5. **Error Handling**: Provide clear error messages and graceful degradation

## Related Components

- **ImpactTool**: Core impact analysis functionality
- **TreeSitter**: AST parsing and symbol analysis
- **Agent System**: Tool execution and session management
- **Configuration System**: Settings and options management