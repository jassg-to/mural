{ pkgs, ... }:

pkgs.testers.nixosTest {
  name = "mural-content";

  nodes.machine =
    { ... }:
    {
      imports = [ ../modules/mural.nix ];
      services.mural.enable = true;
      services.mural.session = "none";
    };

  testScript = ''
    machine.wait_for_unit("multi-user.target")
    machine.succeed("test -d /var/lib/mural/content")
    machine.succeed("test -f /var/lib/mural/content/config.toml")
    machine.succeed("grep -q '\\[slideshow\\]' /var/lib/mural/content/config.toml")

    # an operator edit must survive a subsequent activation (tmpfiles re-run),
    # not just remain untouched from initial provisioning
    machine.succeed("echo '# operator edit' >> /var/lib/mural/content/config.toml")
    machine.succeed("systemd-tmpfiles --create")
    machine.succeed("grep -q '# operator edit' /var/lib/mural/content/config.toml")
  '';
}
