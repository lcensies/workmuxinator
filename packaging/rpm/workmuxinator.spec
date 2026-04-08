Name:           workmuxinator
Version:        @VERSION@
Release:        1%{?dist}
Summary:        Launch workmux worktrees for all tmuxinator projects
License:        MIT
BuildArch:      noarch
Requires:       tmux
Recommends:     yq

%description
workmuxinator is a bash wrapper around workmux and tmuxinator that
reads tmuxinator project configs and automatically opens workmux
worktrees for each project. The 'run' subcommand additionally resumes
the configured AI coding agent (e.g. Claude Code) in each worktree.

%install
mkdir -p %{buildroot}%{_bindir}
install -Dm755 %{_sourcedir}/workmuxinator %{buildroot}%{_bindir}/workmuxinator

%files
%{_bindir}/workmuxinator

%changelog
* Tue Apr 08 2026 workmuxinator contributors
- Initial release 0.1.0
