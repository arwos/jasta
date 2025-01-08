export interface Err {
  msg: string
}

export class ConfigName {
  id: number;
  name: string;
  enable: number;

  constructor() {
    this.id = 0;
    this.name = '';
    this.enable = 0;
  }
}

export interface Config {

}
