import Route from '@ember/routing/route';

export default class VariablesNewRoute extends Route {
  model() {
    return this.store.createRecord('variable');
  }
  resetController(controller, isExiting) {
    if (isExiting) {
      // If user didn't save, delete the freshly created model
      if (controller.model.isNew) {
        controller.model.destroyRecord();
      }
    }
  }
}
