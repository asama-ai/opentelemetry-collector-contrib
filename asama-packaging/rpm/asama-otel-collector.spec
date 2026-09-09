%define _bindir /opt/asama.ai/bin
%define _configdir /opt/asama.ai/config
%define _logdir /opt/asama.ai/logs
%define _servicedir /etc/systemd/system
%define debug_package %{nil}
%define _binary_payload w2.xzdio

Name:           asama-otel-collector
Version:        _RPM_VERSION_
Release:        _RPM_RELEASE_%{?dist}
Summary:        Asama AI OpenTelemetry Collector
License:        Proprietary

Source0:        otelcontribcol-otel-collector
Source1:        otel-collector-config.yaml
Source2:        asama-otel-collector.service

Requires:       systemd
Requires:       asama-host-agent

%description
OpenTelemetry Collector package for Asama AI monitoring stack

%prep
# Nothing to do here - using direct binary files

%install
mkdir -p %{buildroot}%{_bindir}
mkdir -p %{buildroot}%{_configdir}
mkdir -p %{buildroot}%{_logdir}
mkdir -p %{buildroot}%{_servicedir}

install -p -m 750 %{SOURCE0} %{buildroot}%{_bindir}/otelcontribcol-otel-collector
install -p -m 640 %{SOURCE1} %{buildroot}%{_configdir}/otel-collector-config.yaml
install -p -m 640 %{SOURCE2} %{buildroot}%{_servicedir}/asama-otel-collector.service

%files
%defattr(-,root,root,-)
%dir %attr(750,asama-agent,asama-agent) /opt/asama.ai
%dir %attr(750,asama-agent,asama-agent) %{_bindir}
%dir %attr(750,asama-agent,asama-agent) %{_configdir}
%dir %attr(750,asama-agent,asama-agent) %{_logdir}
%attr(750,asama-agent,asama-agent) %{_bindir}/otelcontribcol-otel-collector
%config(noreplace) %attr(640,asama-agent,asama-agent) %{_configdir}/otel-collector-config.yaml
%attr(644,root,root) %{_servicedir}/asama-otel-collector.service

%pre
if ! getent group asama-agent >/dev/null; then
    groupadd --system asama-agent
fi
if ! getent passwd asama-agent >/dev/null; then
    useradd --system \
        --gid asama-agent \
        --no-create-home \
        --shell /sbin/nologin \
        asama-agent
fi

%post
set -e
mkdir -p %{_configdir} || echo "ERROR: Failed to create config dir"
mkdir -p %{_logdir} || echo "ERROR: Failed to create log dir"
chown -R asama-agent:asama-agent %{_configdir}
chown -R asama-agent:asama-agent %{_logdir}
chmod 750 %{_configdir}
chmod 750 %{_logdir}

if [ ! -f %{_configdir}/host-agent.env ]; then
    install -o asama-agent -g asama-agent -m 640 /dev/null %{_configdir}/host-agent.env
fi
if [ ! -f %{_configdir}/device_metadata.env ]; then
    install -o asama-agent -g asama-agent -m 640 /dev/null %{_configdir}/device_metadata.env
fi

systemctl daemon-reload || echo "ERROR: Failed to reload systemd"
systemctl enable asama-otel-collector.service || echo "ERROR: Failed to enable otel-collector service"
systemctl restart asama-otel-collector.service || echo "ERROR: Failed to restart otel-collector service"

%preun
if [ $1 -eq 0 ]; then
    systemctl --no-reload disable --now asama-otel-collector.service >/dev/null 2>&1 || :
fi

%postun
if [ $1 -eq 0 ]; then
    systemctl daemon-reload >/dev/null 2>&1 || :
fi
