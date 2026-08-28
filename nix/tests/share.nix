{ pkgs, ... }:

pkgs.testers.nixosTest {
  name = "mural-share";

  nodes.machine =
    { ... }:
    {
      imports = [ ../modules/mural.nix ];
      services.mural.enable = true;
      services.mural.session = "none";
      services.mural.share.enable = true;
    };

  testScript = ''
    machine.wait_for_unit("samba-smbd.service")
    machine.wait_for_unit("samba-nmbd.service")

    machine.succeed("printf 'testpass\\ntestpass\\n' | ${pkgs.samba}/bin/smbpasswd -a -s mural")

    # authenticated write succeeds
    machine.succeed(
        "${pkgs.samba}/bin/smbclient //localhost/content -U mural%testpass"
        " -c 'put /etc/hostname hostname.txt'"
    )
    machine.succeed("test -f /var/lib/mural/content/hostname.txt")

    # anonymous/guest access is refused
    machine.fail("${pkgs.samba}/bin/smbclient //localhost/content -N -c 'ls'")
  '';
}
