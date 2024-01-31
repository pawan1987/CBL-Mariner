Vendor:         Microsoft Corporation
Distribution:   Mariner
%if %{?WITH_SELINUX:0}%{!?WITH_SELINUX:1}
%global WITH_SELINUX 1
%endif

Summary: Displays the users logged into machines on the local network
Name: rusers
Version: 0.17
Release: 97%{?dist}
License: BSD
Url: http://rstatd.sourceforge.net/
Source: http://ftp.linux.org.uk/pub/linux/Networking/netkit/netkit-rusers-%{version}.tar.gz
Source1: rusersd.service
Source2: rstatd.tar.gz
Source3: rstatd.service
Patch0: rstatd-jbj.patch
Patch1: netkit-rusers-0.15-numusers.patch
Patch2: rusers-0.17-2.4.patch
Patch3: rusers-0.17-includes.patch
Patch4: rusers-0.17-truncate.patch
Patch5: rusers-0.17-stats.patch
Patch6: rusers-0.17-rstatd-no-static-buffer.patch
Patch7: rusers-0.17-strip.patch
Patch8: rusers-0.17-rup.patch
Patch9: rusers-0.17-rup-timeout.patch
Patch10: rusers-0.17-procps.patch
Patch11: rusers-0.17-rup-stack.patch
Patch12: rusers-0.17-bigendian.patch
Patch13: rusers-0.17-return.patch
Patch14: rusers-0.17-procdiskstats.patch
Patch15: rusers-0.17-rusersd-droppriv.patch
# Oracle explicitly gave permission for this relicensing on August 18, 2010.
Patch16: rusers-0.17-new-rpc-license.patch
Patch17: rusers-0.17-manhelp.patch
Patch18: rusers-0.17-freerpc.patch
Patch19: rstatd-man.patch
# Provide the BSD 3-clause license text as COPYING file
Patch20: rusers-0.17-license.patch
BuildRequires: gcc
BuildRequires: procps libselinux-devel
BuildRequires: rpcsvc-proto-devel
BuildRequires: rpcgen
BuildRequires: libtirpc-devel

%description
The rusers program allows users to find out who is logged into various
machines on the local network.  The rusers command produces output
similar to who, but for the specified list of hosts or for all
machines on the local network.

Install rusers if you need to keep track of who is logged into your
local network.

%package server
Summary: Server for the rusers protocol
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd
Requires(post): systemd-sysv
Requires: portmap
BuildRequires: systemd
BuildRequires: systemd-devel

%description server
The rusers program allows users to find out who is logged into various
machines on the local network.  The rusers command produces output
similar to who, but for the specified list of hosts or for all
machines on the local network. The rusers-server package contains the
server for responding to rusers requests.

Install rusers-server if you want remote users to be able to see
who is logged into your machine.

%prep
%setup -q -n netkit-rusers-%{version} -a 2
%patch 0 -p1 -b .jbj
%patch 1 -p1 -b .numusers
%patch 2 -p1 -b .2.4
%patch 3 -p1 -b .includes
%patch 4 -p1 -b .truncate
%patch 5 -p1 -b .stats
%patch 6 -p1 -b .rstatd-no-static-buffer
%patch 7 -p1 -b .strip
%patch 8 -p1 -b .rup
%patch 9 -p1 -b .rup-timeout
%patch 10 -p1 -b .procps
%patch 11 -p1 -b .rup-stack
%patch 12 -p1 -b .bigendian
%patch 13 -p1 -b .return
%patch 14 -p1 -b .procdiskstats
%patch 15 -p1 -b .dropprivs
%patch 16 -p1 -b .licensefix
%patch 17 -p1 -b .manhelp
%patch 18 -p1 -b .freerpc
%patch 19 -p1 -b .rstatd-man
%patch 20 -p1 -b .license

%build
cat > MCONFIG <<EOF
# Generated by configure (confgen version 2) on Wed Jul 17 09:33:22 EDT 2002
#

BINDIR=%{_bindir}
SBINDIR=%{_sbindir}
MANDIR=%{_mandir}
BINMODE=755
DAEMONMODE=755
MANMODE=644
PREFIX=/usr
EXECPREFIX=/usr
INSTALLROOT=
CC=cc
CFLAGS=${RPM_OPT_FLAGS} -I/usr/include/tirpc -fPIC -Wall -W -Wpointer-arith -Wbad-function-cast -Wcast-qual -Wstrict-prototypes -Wmissing-prototypes -Wmissing-declarations -Wnested-externs -Winline
LDFLAGS=-pie -Wl,-z,relro,-z,now -ltirpc
LIBS=-lsystemd
USE_GLIBC=1

EOF

make
%if %{WITH_SELINUX}
make LIBS="-lselinux -lsystemd" -C rpc.rstatd
%else
make -C rpc.rstatd
%endif


%install
mkdir -p ${RPM_BUILD_ROOT}%{_bindir}
mkdir -p ${RPM_BUILD_ROOT}%{_sbindir}
mkdir -p ${RPM_BUILD_ROOT}%{_mandir}/man{1,8}
mkdir -p ${RPM_BUILD_ROOT}%{_unitdir}

make INSTALLROOT=${RPM_BUILD_ROOT} install
make INSTALLROOT=${RPM_BUILD_ROOT} install -C rpc.rstatd

install -m 0644 %SOURCE1 ${RPM_BUILD_ROOT}%{_unitdir}/rusersd.service
install -m 0644 %SOURCE3 ${RPM_BUILD_ROOT}%{_unitdir}/rstatd.service

%post server
%systemd_post rstatd.service
%systemd_post rusersd.service

%preun server
%systemd_preun rstatd.service
%systemd_preun rusersd.service

%postun server
%systemd_postun_with_restart rstatd.service
%systemd_postun_with_restart rusersd.service

%files
%doc README COPYING
%{_bindir}/rup
%{_bindir}/rusers
%{_mandir}/man1/*

%files server
%doc COPYING
%{_mandir}/man8/*
%{_sbindir}/rpc.rstatd
%{_sbindir}/rpc.rusersd
%{_unitdir}/rusersd.service
%{_unitdir}/rstatd.service

%changelog
* Tue Jan 30 2024 Pawel Winogrodzki <pawelwi@microsoft.com> - 0.17-97
- Updating the usage of the '%%patch' macro.

* Fri Oct 15 2021 Pawel Winogrodzki <pawelwi@microsoft.com> - 0.17-96
- Initial CBL-Mariner import from Fedora 32 (license: MIT).

* Thu Jan 30 2020 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-95
- Rebuilt for https://fedoraproject.org/wiki/Fedora_32_Mass_Rebuild

* Fri Jul 26 2019 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-94
- Rebuilt for https://fedoraproject.org/wiki/Fedora_31_Mass_Rebuild

* Sat Feb 02 2019 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-93
- Rebuilt for https://fedoraproject.org/wiki/Fedora_30_Mass_Rebuild

* Tue Jul 24 2018 Petr Kubat <pkubat@redhat.com> - 0.17-92
- Add BuildRequires for gcc (#1606282)

* Sat Jul 14 2018 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-91
- Rebuilt for https://fedoraproject.org/wiki/Fedora_29_Mass_Rebuild

* Thu Mar 29 2018 Petr Kubat <pkubat@redhat.com> - 0.17-90
- Remove explicit requires

* Thu Mar 15 2018 Petr Kubat <pkubat@redhat.com> - 0.17-89
- Add dependencies on libtirpc, rpcsvc-proto-devel and rpcgen (#1556423)
  Related to: https://fedoraproject.org/wiki/Changes/SunRPCRemoval

* Fri Feb 09 2018 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-88
- Rebuilt for https://fedoraproject.org/wiki/Fedora_28_Mass_Rebuild

* Thu Aug 03 2017 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-87
- Rebuilt for https://fedoraproject.org/wiki/Fedora_27_Binutils_Mass_Rebuild

* Thu Jul 27 2017 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-86
- Rebuilt for https://fedoraproject.org/wiki/Fedora_27_Mass_Rebuild

* Tue Feb 21 2017 Petr Kubat <pkubat@redhat.com> - 0.17-85
- Remove execute permissions from *.service files (#1422022)
- Add BSD license text (#1418686)

* Sat Feb 11 2017 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-84
- Rebuilt for https://fedoraproject.org/wiki/Fedora_26_Mass_Rebuild

* Wed Dec 14 2016 Petr Kubat <pkubat@redhat.com> - 0.17-83
- Remove mention of 'rpc.statd' from rstatd man page (#1374436)
- Update upstream source URL

* Thu Feb 04 2016 Fedora Release Engineering <releng@fedoraproject.org> - 0.17-82
- Rebuilt for https://fedoraproject.org/wiki/Fedora_24_Mass_Rebuild

* Fri Jun 19 2015 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-81
- Rebuilt for https://fedoraproject.org/wiki/Fedora_23_Mass_Rebuild

* Mon Aug 18 2014 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-80
- Rebuilt for https://fedoraproject.org/wiki/Fedora_21_22_Mass_Rebuild

* Mon Aug 11 2014 Jakub Dorňák <jdornak@redhat.com> - 0.17-79
- use libsystemd.so instead of the old libsystemd-daemon.so
  Resolves: #1125090

* Sun Jun 08 2014 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-78
- Rebuilt for https://fedoraproject.org/wiki/Fedora_21_Mass_Rebuild

* Fri Jul 26 2013 Honza Horak <hhorak@redhat.com> - 0.17-77
- Free already alocated memory when parsing of RPC request failed
- Require systemd instead of systemd-units
- Remove SysV init converting

* Fri May 24 2013 Honza Horak <hhorak@redhat.com> - 0.17-76
- Remove syslog.target from unit requires
  Resolves: #966009

* Mon May 20 2013 Honza Horak <hhorak@redhat.com> - 0.17-75
- Fix man page vs. help differences

* Thu Feb 14 2013 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-74
- Rebuilt for https://fedoraproject.org/wiki/Fedora_19_Mass_Rebuild

* Fri Nov 30 2012 Honza Horak <hhorak@redhat.com> - 0.17-73
- Build daemons with full relro

* Fri Oct 05 2012 Honza Horak <hhorak@redhat.com> - 0.17-72
- Remove sdnotify message, while it doesn't work with forking service

* Thu Oct 04 2012 Honza Horak <hhorak@redhat.com> - 0.17-71
- Run %%triggerun regardless of systemd_post variable definition

* Tue Sep 11 2012 Honza Horak <hhorak@redhat.com> - 0.17-70
- Minor spec file changes
- Use new systemd macros (Resolves: #850302)
- Use systemd notify messages

* Sat Jul 21 2012 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-69
- Rebuilt for https://fedoraproject.org/wiki/Fedora_18_Mass_Rebuild

* Thu Mar 22 2012 Honza Horak <hhorak@redhat.com> - 0.17-68
- removed PIDFile in both service files
  Resolves: #804891

* Sat Jan 14 2012 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-67
- Rebuilt for https://fedoraproject.org/wiki/Fedora_17_Mass_Rebuild

* Tue Aug 02 2011 Honza Horak <hhorak@redhat.com> - 0.17-66
- added rpcbind ordering dependencies in service files
- fixed systemd related mistakes in spec file

* Tue Aug 02 2011 Honza Horak <hhorak@redhat.com> - 0.17-65
- added rpcbind requirments to service files

* Tue Aug 02 2011 Honza Horak <hhorak@redhat.com> - 0.17-64
- provide systemd native unit files
  Resolves: #722632

* Tue Aug 02 2011 Honza Horak <hhorak@redhat.com> - 0.17-63
- added rpcbind into LSB header
  Resolves: #697862

* Thu Feb 24 2011 Honza Horak <hhorak@redhat.com> - 0.17-62
- fixed rpmlint errors
- Resolves: #634922

* Wed Feb 09 2011 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-61
- Rebuilt for https://fedoraproject.org/wiki/Fedora_15_Mass_Rebuild

* Thu Aug 26 2010 Tom "spot" Callaway <tcallawa@redhat.com> - 0.17-60
- replace SunRPC license with BSD (thanks to Oracle)

* Fri Feb 26 2010 Jiri Moskovcak <jmoskovc@redhat.com> - 0.17-59
- added README

* Wed Feb 24 2010 Jiri Moskovcak <jmoskovc@redhat.com> - 0.17-58
- fixed rusersd initscript
- fixed rstatd initscript
- Resolves: #523368, #523366

* Fri Jan  8 2010 Jiri Moskovcak <jmoskovc@redhat.com> - 0.17-57
- fixed rpmlint warnings

* Sun Jul 26 2009 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-56
- Rebuilt for https://fedoraproject.org/wiki/Fedora_12_Mass_Rebuild

* Wed Feb 25 2009 Fedora Release Engineering <rel-eng@lists.fedoraproject.org> - 0.17-55
- Rebuilt for https://fedoraproject.org/wiki/Fedora_11_Mass_Rebuild

* Thu Sep  4 2008 Jiri Moskovcak <jmoskovc@redhat.com> - 0.17-54
- modified truncate patch to work with fuzz=0

* Tue Feb 19 2008 Fedora Release Engineering <rel-eng@fedoraproject.org> - 0.17-53
- Autorebuild for GCC 4.3

* Tue Sep 18 2007 Jiri Moskovcak <jmoskovc@redhat.com> 0.17-52
- Fixed init script to work properly with rpcbind

* Sat Sep 15 2007 Steve Dickson <steved@redaht.com> 0.17-51
- Removed portmap dependency and re-worked when the user
  privilege are drop; allowing port registration with
  rpcbind. (#247985)

* Wed Aug 29 2007 Fedora Release Engineering <rel-eng at fedoraproject dot org> - 0.17-50
- Rebuild for selinux ppc32 issue.

* Wed Jul 25 2007 Jeremy Katz <katzj@redhat.com> - 0.17-49
- rebuild for toolchain bug

* Mon Jul 23 2007 Jiri Moskovcak <jmoskovc@redhat.com> 0.17-48
- Fixed init scripts to comply with LSB standard
- Resolves: #247047

* Wed Aug 09 2006 Phil Knirsch <pknirsch@redhat.com> 0.17-47
- Modified the RHEL3 procpartitions patch to work on recent 2.6 kernels (#201839)

* Wed Jul 12 2006 Jesse Keating <jkeating@redhat.com> - 0.17-46.1
- rebuild

* Tue Mar 21 2006 Phil Knirsch <pknirsch@redhat.com> - 0.17-46
- Included fix for correct return values for rup (#177419)

* Fri Feb 10 2006 Jesse Keating <jkeating@redhat.com> - 0.17-45.2.1
- bump again for double-long bug on ppc(64)

* Tue Feb 07 2006 Jesse Keating <jkeating@redhat.com> - 0.17-45.2
- rebuilt for new gcc4.1 snapshot and glibc changes

* Fri Dec 09 2005 Jesse Keating <jkeating@redhat.com>
- rebuilt

* Wed Sep 07 2005 Phil Knirsch <pknirsch@redhat.com> 0.17-45
- Fixed 64bit bigendian problem in rpc.rstatd (#130286)

* Wed May 04 2005 Phil Knirsch <pknirsch@redhat.com> 0.17-44
- Fixed rup stack problem (#154396)

* Wed Mar 02 2005 Phil Knirsch <pknirsch@redhat.com> 0.17-43
- bump release and rebuild with gcc 4

* Fri Feb 18 2005 Phil Knirsch <pknirsch@redhat.com> 0.17-42
- rebuilt

* Mon Jul 12 2004 Phil Knirsch <pknirsch@redhat.com> 0.17-41
- Bump release.

* Mon Jul 12 2004 Phil Knirsch <pknirsch@redhat.com> 0.17-40
- Made patch to make rpc.rstatd independant of procps (#127512)

* Tue Jun 29 2004 Phil Knirsch <pknirsch@redhat.com> 0.17-39
- Added libselinux-devel BuildPreqreq (#124283).

* Tue Jun 15 2004 Elliot Lee <sopwith@redhat.com>
- rebuilt

* Wed Feb 25 2004 Phil Knirsch <pknirsch@redhat.com> 0.17-37
- rebuilt against latest procps lib.
- built stuff with PIE enabled.

* Fri Feb 13 2004 Elliot Lee <sopwith@redhat.com>
- rebuilt

* Fri Jan 23 2004 Bill Nottingham <notting@redhat.com> 0.17-35
- rebuild against new libproc
- selinux is the default, remove the release suffix

* Tue Oct 21 2003 Dan Walsh <dwalsh@redhat.com> 0.17-34.sel
- remove -lattr

* Thu Oct 09 2003 Dan Walsh <dwalsh@redhat.com> 0.17-33.sel
- turn selinux on

* Fri Oct 03 2003 Florian La Roche <Florian.LaRoche@redhat.de>
- rebuild

* Mon Sep 08 2003 Dan Walsh <dwalsh@redhat.com> 0.17-31.4
- turn selinux off

* Fri Sep 05 2003 Dan Walsh <dwalsh@redhat.com> 0.17-31.3.sel
- turn selinux on

* Mon Jul 28 2003 Dan Walsh <dwalsh@redhat.com> 0.17-31.2
- Add SELinux library support

* Mon Jul 14 2003 Tim Powers <timp@redhat.com> 0.17-31.1
- rebuilt for RHEL

* Mon Jul 07 2003 Elliot Lee <sopwith@redhat.com>
- Rebuild

* Wed Jun 04 2003 Elliot Lee <sopwith@redhat.com>
- rebuilt

* Fri May 23 2003 Tim Powers <timp@redhat.com> 0.17-29
- rebuilt

* Wed May 21 2003 Matt Wilson <msw@redhat.com> 0.17-28
- rebuilt

* Wed May 21 2003 Matt Wilson <msw@redhat.com> 0.17-27
- added netkit-rusers-0.17-rup-timeout.patch to fix immediate timeout
  problem in rup (#91322)

* Wed Jan 22 2003 Tim Powers <timp@redhat.com>
- rebuilt

* Tue Jan 21 2003 Phil Knirsch <pknirsch@redhat.com> 0.17-24
- Bumped release and rebuilt due to new procps.

* Mon Dec 02 2002 Elliot Lee <sopwith@redhat.com> 0.17-23
- Fix multilib

* Tue Nov  5 2002 Nalin Dahyabhai <nalin@redhat.com> 0.17-22
- Bumped release and rebuilt due to procps update.
- s/Copyright/License/g

* Thu Aug 08 2002 Phil Knirsch <pknirsch@redhat.com> 0.17-21
- Bumped release and rebuilt due to procps update.

* Wed Jul 17 2002 Phil Knirsch <pknirsch@redhat.com> 0.17-20
- Fixed the sort ordering for rup -l host1 host2 host3 (#67551)
- Don't use configure anymore, doesn't work in build environment correctly.

* Fri Jun 21 2002 Tim Powers <timp@redhat.com> 0.17-19
- automated rebuild

* Wed Jun 19 2002 Phil Knirsch <pknirsch@redhat.com> 0.17-18
- Actually applied Matt's patch ;-)
- Don't forcibly strip binaries

* Mon Jun 10 2002 Matt Wilson <msw@redhat.com>
- fixed static buffer size which truncated /proc/stat when it was more
  than 1024 bytes (#64935)

* Tue Jun 04 2002 Phil Knirsch <pknirsch@redhat.com>
- bumped release number and rebuild

* Thu May 23 2002 Tim Powers <timp@redhat.com>
- automated rebuild

* Wed Jan 23 2002 Phil Knirsch <pknirsch@redhat.com>
- Fixed the wrong uptime problem introduced by fixing bug #53244.
- Fixed segfault problem on alpha (and other archs) (bug #53309).

* Thu Jan 17 2002 Phil Knirsch <pknirsch@redhat.com>
- Fixed bug #17065 where rusersd wrongly terminated each string with a '\0'.
- Fixed bug #53244. Now stats for the different protocols are stored separately.

* Wed Jan 09 2002 Tim Powers <timp@redhat.com>
- automated rebuild

* Wed Jul 25 2001 Phil Knirsch <pknirsch@redhat.de>
- Fixed missing includes for time.h and several others (#49887)

* Wed Jun 27 2001 Philipp Knirsch <pknirsch@redhat.de>
- Fixed rstatd.init script to use $0 in usage string (#26553)

* Wed Apr  4 2001 Jakub Jelinek <jakub@redhat.com>
- don't let configure to guess compiler, it can pick up egcs

* Wed Feb 14 2001 Nalin Dahyabhai <nalin@redhat.com>
- merge in Bob Matthews' patch, which solves other parts on 2.4 (#26447)

* Tue Feb  6 2001 Nalin Dahyabhai <nalin@redhat.com>
- don't die if /proc/stat looks a little odd (#25519)

* Mon Feb  5 2001 Bernhard Rosenkraenzer <bero@redhat.com>
- i18nize rstatd init script

* Wed Jan 24 2001 Nalin Dahyabhai <nalin@redhat.com>
- gettextize init script

* Sat Aug 05 2000 Bill Nottingham <notting@redhat.com>
- condrestart fixes

* Thu Jul 20 2000 Bill Nottingham <notting@redhat.com>
- move initscript back

* Sun Jul 16 2000 Matt Wilson <msw@redhat.com>
- rebuilt against new procps

* Wed Jul 12 2000 Prospector <bugzilla@redhat.com>
- automatic rebuild

* Mon Jul 10 2000 Preston Brown <pbrown@redhat.com>
- move initscripts

* Sun Jun 18 2000 Jeff Johnson <jbj@redhat.com>
- FHS packaging.
- update to 0.17.

* Wed Feb  9 2000 Jeff Johnson <jbj@redhat.com>
- compress man pages (again).

* Wed Feb 02 2000 Cristian Gafton <gafton@redhat.com>
- fix description and summary
- man pages are compressed

* Tue Jan  4 2000 Bill Nottingham <notting@redhat.com>
- split client and server

* Tue Dec 21 1999 Jeff Johnson <jbj@redhat.com>
- update to 0.16.

* Wed Nov 10 1999 Bill Nottingham <notting@redhat.com>
- rebuild against new procps

* Wed Sep 22 1999 Jeff Johnson <jbj@redhat.com>
- rusers init script started rstatd.

* Mon Sep 20 1999 Jeff Johnson <jbj@redhat.com>
- (re-)initialize number of users (#5244).

* Fri Aug 27 1999 Preston Brown <pbrown@redhat.com>
- initscripts check for portmapper running before starting (#2615)

* Fri Aug 27 1999 Jeff Johnson <jbj@redhat.com>
- return monitoring statistics like solaris does (#4237).

* Thu Aug 26 1999 Jeff Johnson <jbj@redhat.com>
- update to netkit-0.15.
- on startup, rpc.rstatd needs to read information twice (#3994).

* Mon Aug 16 1999 Bill Nottingham <notting@redhat.com>
- initscript munging

* Tue Apr  6 1999 Jeff Johnson <jbj@redhat.com>
- add rpc.rstatd (#2000)

* Sun Mar 21 1999 Cristian Gafton <gafton@redhat.com> 
- auto rebuild in the new build environment (release 22)

* Mon Mar 15 1999 Jeff Johnson <jbj@redhat.com>
- compile for 6.0.

* Tue May 05 1998 Cristian Gafton <gafton@redhat.com>
- added /etc/rc.d/init.d/functions to the init script

* Tue May 05 1998 Prospector System <bugs@redhat.com>
- translations modified for de, fr, tr

* Sat May 02 1998 Cristian Gafton <gafton@redhat.com>
- enhanced initscript

* Tue Oct 21 1997 Erik Troan <ewt@redhat.com>
- added init script
- users attr
- supports chkconfig

* Tue Jul 15 1997 Erik Troan <ewt@redhat.com>
- initial build
